// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type listUsersPageResponse struct {
	Data struct {
		ListUsers struct {
			Total int `json:"total"`
			Users []struct {
				URN string `json:"urn"`
			} `json:"users"`
		} `json:"listUsers"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ListCorpUserURNs returns the URNs of all corp users in DataHub.
//
// The underlying listUsers GraphQL query is backed by OpenSearch and is
// eventually consistent. Entities created within the last few seconds may not
// appear. This function is intended for enumeration (extract tooling, inventory
// data sources), not for authoritative reads -- use GetUserByURN for those.
func (c *Client) ListCorpUserURNs(ctx context.Context) ([]string, error) {
	const q = `
query listUsers($input: ListUsersInput!) {
  listUsers(input: $input) {
    total
    users {
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
			return nil, fmt.Errorf("unexpected HTTP %d from DataHub listUsers", res.StatusCode)
		}

		var gqlResp listUsersPageResponse
		decodeErr := json.NewDecoder(res.Body).Decode(&gqlResp)
		res.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("parsing listUsers response: %w", decodeErr)
		}
		if len(gqlResp.Errors) > 0 {
			return nil, fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
		}

		page := gqlResp.Data.ListUsers.Users
		for _, u := range page {
			urns = append(urns, u.URN)
		}

		start += len(page)
		if start >= gqlResp.Data.ListUsers.Total || len(page) == 0 {
			break
		}
	}

	return urns, nil
}
