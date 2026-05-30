// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type listGroupsPageResponse struct {
	Data struct {
		ListGroups struct {
			Total  int `json:"total"`
			Groups []struct {
				URN string `json:"urn"`
			} `json:"groups"`
		} `json:"listGroups"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ListGroupURNs returns the URNs of all corp groups in DataHub.
//
// The underlying listGroups GraphQL query is backed by OpenSearch and is
// eventually consistent. Entities created within the last few seconds may not
// appear. This function is intended for enumeration (extract tooling, inventory
// data sources), not for authoritative reads -- use GetGroupByURN for those.
func (c *Client) ListGroupURNs(ctx context.Context) ([]string, error) {
	const q = `
query listGroups($input: ListGroupsInput!) {
  listGroups(input: $input) {
    total
    groups {
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
			return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_USERS_AND_GROUPS privilege", res.StatusCode)
		}
		if res.StatusCode >= http.StatusBadRequest {
			res.Body.Close()
			return nil, fmt.Errorf("unexpected HTTP %d from DataHub listGroups", res.StatusCode)
		}

		var gqlResp listGroupsPageResponse
		decodeErr := json.NewDecoder(res.Body).Decode(&gqlResp)
		res.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("parsing listGroups response: %w", decodeErr)
		}
		if len(gqlResp.Errors) > 0 {
			return nil, fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
		}

		page := gqlResp.Data.ListGroups.Groups
		for _, g := range page {
			urns = append(urns, g.URN)
		}

		start += len(page)
		if start >= gqlResp.Data.ListGroups.Total || len(page) == 0 {
			break
		}
	}

	return urns, nil
}
