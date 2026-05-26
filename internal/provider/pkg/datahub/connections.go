// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// SECURITY: All connection mutations in this file use the DataHub GraphQL API
// (/api/graphql), NOT the OpenAPI v3 path. The DataHub server encrypts the
// connection blob server-side (AES-GCM-256) inside ConnectionService.upsertConnection().
// OpenAPI write endpoints bypass this encryption logic.
//
// Reads via the OpenAPI v3 entity endpoint return the encrypted blob, which the
// provider cannot decrypt. Only top-level metadata (name, platform) are
// available from the read path; all per-platform config fields are opaque.

package datahub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Connection is the read-shape returned by GetConnectionByURN.
// The config blob is encrypted at rest and not included; only
// top-level metadata is available from the read path.
type Connection struct {
	URN      string
	ID       string
	Name     string
	Platform string // platform URN suffix (e.g., "databricks")
}

// UpsertConnectionInput groups the inputs for upserting a DataHub connection.
// Blob is a JSON string containing the per-platform configuration. It is sent
// directly to DataHub, which encrypts it before persisting.
type UpsertConnectionInput struct {
	// URN is the full connection URN. Empty on create; must be set on update.
	URN  string
	Name string
	// Platform is the URN suffix (e.g., "databricks"), not the full URN.
	Platform string
	// Blob is the platform config serialized as a JSON string.
	Blob string
}

type upsertConnectionResponse struct {
	Data struct {
		UpsertConnection struct {
			URN string `json:"urn"`
		} `json:"upsertConnection"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type deleteConnectionResponse struct {
	Data struct {
		DeleteConnection bool `json:"deleteConnection"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// connectionEntity is the OpenAPI v3 response shape for
// GET /openapi/v3/entity/datahubconnection/{urn}.
type connectionEntity struct {
	URN                      string                   `json:"urn"`
	DataHubConnectionKey     *connectionKeyAspect     `json:"dataHubConnectionKey,omitempty"`
	DataHubConnectionDetails *connectionDetailsAspect `json:"dataHubConnectionDetails,omitempty"`
}

type connectionKeyAspect struct {
	Value connectionKey `json:"value"`
}

type connectionKey struct {
	ID string `json:"id"`
}

type connectionDetailsAspect struct {
	Value connectionDetailsData `json:"value"`
}

type connectionDetailsData struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Platform string `json:"platform,omitempty"`
}

// UpsertConnection creates or updates a DataHub connection via the GraphQL API
// and returns the URN of the upserted connection.
func (c *Client) UpsertConnection(ctx context.Context, in UpsertConnectionInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return "", errors.New("name is required")
	}
	if in.Platform == "" {
		return "", errors.New("platform is required")
	}
	if in.Blob == "" {
		return "", errors.New("blob is required")
	}

	const q = `
mutation upsertConnection($input: UpsertDataHubConnectionInput!) {
  upsertConnection(input: $input) {
    urn
  }
}`

	inputVars := map[string]any{
		"name":     in.Name,
		"type":     "JSON",
		"platform": "urn:li:dataPlatform:" + in.Platform,
		"details":  map[string]any{"blob": in.Blob},
	}
	if in.URN != "" {
		inputVars["urn"] = in.URN
	}

	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"input": inputVars},
	}

	req, err := c.NewRequest(ctx, http.MethodPost, "/api/graphql", body)
	if err != nil {
		return "", err
	}

	res, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_CONNECTIONS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("unexpected HTTP %d from DataHub connections API", res.StatusCode)
	}

	var gqlResp upsertConnectionResponse
	if err := json.NewDecoder(res.Body).Decode(&gqlResp); err != nil {
		return "", fmt.Errorf("parsing upsertConnection response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		return "", fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}

	urn := gqlResp.Data.UpsertConnection.URN
	if urn == "" {
		// Construct from the connection_id we sent.
		id := strings.TrimPrefix(in.URN, "urn:li:dataHubConnection:")
		urn = "urn:li:dataHubConnection:" + id
	}
	return urn, nil
}

// GetConnectionByURN fetches a DataHub connection directly by URN via the
// OpenAPI v3 entity endpoint, which reads from the primary datastore (MySQL)
// rather than the search index. Returns nil (no error) on HTTP 404.
//
// Only top-level metadata (name, platform) are returned; the per-platform
// config blob is encrypted at rest and not available in the response.
func (c *Client) GetConnectionByURN(ctx context.Context, urn string) (*Connection, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/datahubconnection/%s", urn)
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_CONNECTIONS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub connections API: %s", res.StatusCode, respBody)
	}

	var entity connectionEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing connection entity response: %w", err)
	}

	if entity.DataHubConnectionDetails == nil {
		return nil, nil
	}

	id := ""
	if entity.DataHubConnectionKey != nil {
		id = entity.DataHubConnectionKey.Value.ID
	}
	if id == "" {
		id = strings.TrimPrefix(entity.URN, "urn:li:dataHubConnection:")
	}

	platform := strings.TrimPrefix(entity.DataHubConnectionDetails.Value.Platform, "urn:li:dataPlatform:")

	return &Connection{
		URN:      entity.URN,
		ID:       id,
		Name:     entity.DataHubConnectionDetails.Value.Name,
		Platform: platform,
	}, nil
}

// DeleteConnection deletes a DataHub connection by URN via the GraphQL API.
// Returns nil if the connection is already gone (idempotent).
func (c *Client) DeleteConnection(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("URN is required")
	}

	const q = `
mutation deleteConnection($urn: String!) {
  deleteConnection(urn: $urn)
}`

	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"urn": urn},
	}

	req, err := c.NewRequest(ctx, http.MethodPost, "/api/graphql", body)
	if err != nil {
		return err
	}

	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_CONNECTIONS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("unexpected HTTP %d from DataHub connections API", res.StatusCode)
	}

	var gqlResp deleteConnectionResponse
	if err := json.NewDecoder(res.Body).Decode(&gqlResp); err != nil {
		return fmt.Errorf("parsing deleteConnection response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		msg := gqlResp.Errors[0].Message
		lower := strings.ToLower(msg)
		if strings.Contains(lower, "not found") || strings.Contains(lower, "does not exist") {
			return nil
		}
		return fmt.Errorf("DataHub API error: %s", msg)
	}
	return nil
}
