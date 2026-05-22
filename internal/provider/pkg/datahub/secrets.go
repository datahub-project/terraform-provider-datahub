// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// SECURITY: All secret mutations in this file use the DataHub GraphQL API
// (/api/graphql), NOT the OpenAPI v3 path (/openapi/v3/entity/datahubsecret).
// The OpenAPI path bypasses SecretService.encrypt() and stores the secret
// value as plaintext. The GraphQL resolvers (CreateSecretResolver,
// UpdateSecretResolver) run server-side AES-GCM-256 encryption before
// persisting the value.
//
// Reads via the OpenAPI v3 path are safe: the secret value is never returned
// in plaintext by any GET response. Only name and description are present.

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

// Secret is the read-shape returned by listSecrets.
// The value field is intentionally absent: DataHub encrypts it server-side
// and never returns the plaintext.
type Secret struct {
	URN         string
	Name        string
	Description string
}

// CreateSecretInput groups the inputs for creating a DataHub secret.
type CreateSecretInput struct {
	Name        string
	Value       string
	Description string
}

// UpdateSecretInput groups the inputs for updating a DataHub secret.
// Note: the DataHub resolver requires Value on every update; there is no
// description-only path.
type UpdateSecretInput struct {
	URN         string
	Name        string
	Value       string
	Description string
}

type createSecretResponse struct {
	Data struct {
		CreateSecret string `json:"createSecret"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type updateSecretResponse struct {
	Data struct {
		UpdateSecret string `json:"updateSecret"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type deleteSecretResponse struct {
	Data struct {
		DeleteSecret string `json:"deleteSecret"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type listSecretsResponse struct {
	Data struct {
		ListSecrets struct {
			Secrets []struct {
				URN         string `json:"urn"`
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"secrets"`
		} `json:"listSecrets"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// secretEntity is the OpenAPI v3 response shape for GET /openapi/v3/entity/datahubsecret/{urn}.
// The encrypted secret value field inside dataHubSecretValue is intentionally ignored.
type secretEntity struct {
	URN                string             `json:"urn"`
	DataHubSecretKey   *secretKeyAspect   `json:"dataHubSecretKey,omitempty"`
	DataHubSecretValue *secretValueAspect `json:"dataHubSecretValue,omitempty"`
}

type secretKeyAspect struct {
	Value secretKey `json:"value"`
}

type secretKey struct {
	ID string `json:"id"`
}

type secretValueAspect struct {
	Value secretValueData `json:"value"`
}

type secretValueData struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// encrypted Value field is present in the response but intentionally ignored
}

// CreateSecret creates a DataHub secret via the GraphQL API and returns the
// URN of the created secret.
func (c *Client) CreateSecret(ctx context.Context, in CreateSecretInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return "", errors.New("name is required")
	}
	if in.Value == "" {
		return "", errors.New("value is required")
	}

	const q = `
mutation createSecret($input: CreateSecretInput!) {
  createSecret(input: $input)
}`

	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"name":        in.Name,
				"value":       in.Value,
				"description": in.Description,
			},
		},
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
		return "", fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_SECRETS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("unexpected HTTP %d from DataHub secrets API", res.StatusCode)
	}

	var gqlResp createSecretResponse
	if err := json.NewDecoder(res.Body).Decode(&gqlResp); err != nil {
		return "", fmt.Errorf("parsing createSecret response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		msg := gqlResp.Errors[0].Message
		if strings.Contains(msg, "This Secret already exists") {
			return "", fmt.Errorf(
				"secret %q already exists in DataHub; import it with "+
					"`terraform import datahub_secret.<label> urn:li:dataHubSecret:%s` "+
					"or choose a different name",
				in.Name, in.Name,
			)
		}
		return "", fmt.Errorf("DataHub API error: %s", msg)
	}

	urn := gqlResp.Data.CreateSecret
	if urn == "" {
		urn = fmt.Sprintf("urn:li:dataHubSecret:%s", in.Name)
	}
	return urn, nil
}

// GetSecretByURN fetches a DataHub secret directly by URN via the OpenAPI v3
// entity endpoint, which reads from the primary datastore (MySQL) rather than
// the search index. Use this in Read to avoid the eventual-consistency lag that
// affects listSecrets (which goes through OpenSearch).
// Returns nil (no error) when the URN does not exist (HTTP 404).
func (c *Client) GetSecretByURN(ctx context.Context, urn string) (*Secret, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/datahubsecret/%s", urn)
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
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_SECRETS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub secrets API: %s", res.StatusCode, respBody)
	}

	var entity secretEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing secret entity response: %w", err)
	}

	if entity.DataHubSecretValue == nil {
		return nil, nil
	}

	name := entity.DataHubSecretValue.Value.Name
	if name == "" && entity.DataHubSecretKey != nil {
		name = entity.DataHubSecretKey.Value.ID
	}

	return &Secret{
		URN:         entity.URN,
		Name:        name,
		Description: entity.DataHubSecretValue.Value.Description,
	}, nil
}

// GetSecretByName looks up a DataHub secret by exact name using listSecrets.
// Returns nil (no error) when the secret is not found.
func (c *Client) GetSecretByName(ctx context.Context, name string) (*Secret, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("name is required")
	}

	const q = `
query listSecrets($input: ListSecretsInput!) {
  listSecrets(input: $input) {
    secrets {
      urn
      name
      description
    }
  }
}`

	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"start": 0,
				"count": 100,
				"query": name,
			},
		},
	}

	req, err := c.NewRequest(ctx, http.MethodPost, "/api/graphql", body)
	if err != nil {
		return nil, err
	}

	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_SECRETS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub secrets API", res.StatusCode)
	}

	var gqlResp listSecretsResponse
	if err := json.NewDecoder(res.Body).Decode(&gqlResp); err != nil {
		return nil, fmt.Errorf("parsing listSecrets response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}

	// listSecrets uses a fuzzy/substring search -- filter for exact name match.
	for _, s := range gqlResp.Data.ListSecrets.Secrets {
		if s.Name == name {
			return &Secret{
				URN:         s.URN,
				Name:        s.Name,
				Description: s.Description,
			}, nil
		}
	}
	return nil, nil
}

// UpdateSecret updates an existing DataHub secret via the GraphQL API.
func (c *Client) UpdateSecret(ctx context.Context, in UpdateSecretInput) error {
	if c == nil {
		return errors.New("client is nil")
	}
	in.URN = strings.TrimSpace(in.URN)
	if in.URN == "" {
		return errors.New("URN is required")
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errors.New("name is required")
	}
	if in.Value == "" {
		return errors.New("value is required")
	}

	const q = `
mutation updateSecret($input: UpdateSecretInput!) {
  updateSecret(input: $input)
}`

	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"urn":         in.URN,
				"name":        in.Name,
				"value":       in.Value,
				"description": in.Description,
			},
		},
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
		return fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_SECRETS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("unexpected HTTP %d from DataHub secrets API", res.StatusCode)
	}

	var gqlResp updateSecretResponse
	if err := json.NewDecoder(res.Body).Decode(&gqlResp); err != nil {
		return fmt.Errorf("parsing updateSecret response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	return nil
}

// DeleteSecret deletes a DataHub secret by URN via the GraphQL API.
// Returns nil if the secret is already gone (idempotent).
func (c *Client) DeleteSecret(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("URN is required")
	}

	const q = `
mutation deleteSecret($urn: String!) {
  deleteSecret(urn: $urn)
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
		return fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_SECRETS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("unexpected HTTP %d from DataHub secrets API", res.StatusCode)
	}

	var gqlResp deleteSecretResponse
	if err := json.NewDecoder(res.Body).Decode(&gqlResp); err != nil {
		return fmt.Errorf("parsing deleteSecret response: %w", err)
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
