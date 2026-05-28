// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type listSecretsPageResponse struct {
	Data struct {
		ListSecrets struct {
			Total   int `json:"total"`
			Secrets []struct {
				URN string `json:"urn"`
			} `json:"secrets"`
		} `json:"listSecrets"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ListSecretURNs returns the URNs of all secrets in DataHub.
//
// The underlying listSecrets GraphQL query is backed by OpenSearch and is
// eventually consistent. Entities created within the last few seconds may not
// appear. This function is intended for enumeration (export tooling, inventory
// data sources), not for authoritative reads -- use GetSecretByURN for those.
func (c *Client) ListSecretURNs(ctx context.Context) ([]string, error) {
	const q = `
query listSecrets($input: ListSecretsInput!) {
  listSecrets(input: $input) {
    total
    secrets {
      urn
    }
  }
}`

	const pageSize = 100
	var urns []string
	start := 0

	for {
		body := map[string]any{
			"query": q,
			"variables": map[string]any{
				"input": map[string]any{
					"start": start,
					"count": pageSize,
					"query": "*",
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

		if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
			res.Body.Close()
			return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_SECRETS privilege", res.StatusCode)
		}
		if res.StatusCode >= http.StatusBadRequest {
			res.Body.Close()
			return nil, fmt.Errorf("unexpected HTTP %d from DataHub listSecrets", res.StatusCode)
		}

		var gqlResp listSecretsPageResponse
		decodeErr := json.NewDecoder(res.Body).Decode(&gqlResp)
		res.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("parsing listSecrets response: %w", decodeErr)
		}
		if len(gqlResp.Errors) > 0 {
			return nil, fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
		}

		page := gqlResp.Data.ListSecrets.Secrets
		for _, s := range page {
			urns = append(urns, s.URN)
		}

		start += len(page)
		if start >= gqlResp.Data.ListSecrets.Total || len(page) == 0 {
			break
		}
	}

	return urns, nil
}
