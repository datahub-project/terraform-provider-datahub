// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type listPoliciesPageResponse struct {
	Data struct {
		ListPolicies struct {
			Total    int `json:"total"`
			Policies []struct {
				URN string `json:"urn"`
			} `json:"policies"`
		} `json:"listPolicies"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ListPolicyURNs returns the URNs of all policies in DataHub.
//
// The underlying listPolicies GraphQL query is backed by OpenSearch and is
// eventually consistent. Entities created within the last few seconds may not
// appear. This function is intended for enumeration (extract tooling, inventory
// data sources), not for authoritative reads -- use GetPolicyByURN for those.
func (c *Client) ListPolicyURNs(ctx context.Context) ([]string, error) {
	const q = `
query listPolicies($input: ListPoliciesInput!) {
  listPolicies(input: $input) {
    total
    policies {
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
			return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_POLICIES privilege", res.StatusCode)
		}
		if res.StatusCode >= http.StatusBadRequest {
			res.Body.Close()
			return nil, fmt.Errorf("unexpected HTTP %d from DataHub listPolicies", res.StatusCode)
		}

		var gqlResp listPoliciesPageResponse
		decodeErr := json.NewDecoder(res.Body).Decode(&gqlResp)
		res.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("parsing listPolicies response: %w", decodeErr)
		}
		if len(gqlResp.Errors) > 0 {
			return nil, fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
		}

		page := gqlResp.Data.ListPolicies.Policies
		for _, p := range page {
			urns = append(urns, p.URN)
		}

		start += len(page)
		if start >= gqlResp.Data.ListPolicies.Total || len(page) == 0 {
			break
		}
	}

	return urns, nil
}
