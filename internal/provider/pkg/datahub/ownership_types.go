// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OwnershipType is the read-shape returned by GetOwnershipTypeByURN.
type OwnershipType struct {
	URN         string
	ID          string
	Name        string
	Description string
}

// ownershipTypeEntity is the OpenAPI v3 response shape for
// GET /openapi/v3/entity/ownershiptype/{urn}.
type ownershipTypeEntity struct {
	URN     string `json:"urn"`
	KeyData *struct {
		Value struct {
			ID string `json:"id"`
		} `json:"value"`
	} `json:"ownershipTypeKey,omitempty"`
	Info *struct {
		Value struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"value"`
	} `json:"ownershipTypeInfo,omitempty"`
}

type deleteOwnershipTypeResponse struct {
	Data struct {
		DeleteOwnershipType bool `json:"deleteOwnershipType"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// WriteOwnershipTypeInfo creates or updates the ownershipTypeInfo aspect of a
// DataHub ownership type via the OpenAPI v3 entity collection endpoint.
//
// This is the correct write path for this entity type. The GraphQL
// createOwnershipType mutation generates a server-side random UUID for the id,
// making the URN non-deterministic and unmanageable by Terraform. Writing the
// aspect directly with a user-supplied URN (and matching urn:li:ownershipType:<id>
// key) matches the DataHub Python SDK convention and produces stable, importable
// URNs.
//
// AuditStamps (created / lastModified) are required fields in the
// OwnershipTypeInfo schema. Both are set to the current time with the system
// actor on every write.
func (c *Client) WriteOwnershipTypeInfo(ctx context.Context, urn, name, description string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	name = strings.TrimSpace(name)
	if urn == "" {
		return errors.New("URN is required")
	}
	if name == "" {
		return errors.New("name is required")
	}

	nowMs := time.Now().UnixMilli()
	auditStamp := map[string]any{
		"time":  nowMs,
		"actor": "urn:li:corpuser:__datahub_system",
	}

	infoValue := map[string]any{
		"name":         name,
		"created":      auditStamp,
		"lastModified": auditStamp,
	}
	if description != "" {
		infoValue["description"] = description
	}

	payload := []map[string]any{
		{
			"urn": urn,
			"ownershipTypeInfo": map[string]any{
				"value": infoValue,
			},
		},
	}

	req, err := c.NewRequest(ctx, http.MethodPost, "/openapi/v3/entity/ownershiptype?async=false", payload)
	if err != nil {
		return fmt.Errorf("building ownership type write request: %w", err)
	}

	res, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("ownership type write request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_OWNERSHIP_TYPES privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("unexpected HTTP %d from DataHub ownership type write API: %s", res.StatusCode, respBody)
	}
	return nil
}

// GetOwnershipTypeByURN fetches a DataHub ownership type directly by URN via
// the OpenAPI v3 entity endpoint (MySQL, strongly consistent). Returns nil (no
// error) on 404.
func (c *Client) GetOwnershipTypeByURN(ctx context.Context, urn string) (*OwnershipType, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/ownershiptype/%s", urn)
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
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_OWNERSHIP_TYPES privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub ownership type API: %s", res.StatusCode, respBody)
	}

	var entity ownershipTypeEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing ownership type entity response: %w", err)
	}

	if entity.KeyData == nil && entity.Info == nil {
		return nil, nil
	}

	id := ""
	if entity.KeyData != nil {
		id = entity.KeyData.Value.ID
	}
	if id == "" {
		id = strings.TrimPrefix(entity.URN, "urn:li:ownershipType:")
	}

	ot := &OwnershipType{URN: entity.URN, ID: id}
	if entity.Info != nil {
		ot.Name = entity.Info.Value.Name
		ot.Description = entity.Info.Value.Description
	}
	return ot, nil
}

// DeleteOwnershipType hard-deletes a DataHub ownership type by URN via the
// deleteOwnershipType GraphQL mutation.
//
// System ownership types (URN id prefix __system__) are protected server-side
// and will return an error. The resource schema validator also rejects
// type_id values beginning with __system__ at plan time.
func (c *Client) DeleteOwnershipType(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("URN is required")
	}

	const q = `
mutation deleteOwnershipType($urn: String!) {
  deleteOwnershipType(urn: $urn)
}`
	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"urn": urn},
	}
	var gqlResp deleteOwnershipTypeResponse
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return err
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	return nil
}
