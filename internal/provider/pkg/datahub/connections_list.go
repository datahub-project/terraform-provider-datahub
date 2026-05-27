// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type searchAcrossEntitiesResponse struct {
	Data struct {
		SearchAcrossEntities struct {
			Total         int `json:"total"`
			SearchResults []struct {
				Entity struct {
					URN string `json:"urn"`
				} `json:"entity"`
			} `json:"searchResults"`
		} `json:"searchAcrossEntities"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ListConnectionURNs returns the URNs of all DataHub connections.
//
// Uses searchAcrossEntities with entity type DATAHUB_CONNECTION. The search
// index is backed by OpenSearch and is eventually consistent. This function
// is intended for enumeration (import tooling, inventory data sources), not
// for authoritative reads -- use GetConnectionByURN for those.
func (c *Client) ListConnectionURNs(ctx context.Context) ([]string, error) {
	return listURNsByEntityType(ctx, c, "DATAHUB_CONNECTION")
}

// listURNsByEntityType is the generic paged search helper used by per-type
// list functions that have no dedicated GraphQL list query.
func listURNsByEntityType(ctx context.Context, c *Client, entityType string) ([]string, error) {
	const q = `
query searchAcrossEntities($input: SearchAcrossEntitiesInput!) {
  searchAcrossEntities(input: $input) {
    total
    searchResults {
      entity {
        urn
      }
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
					"types": []string{entityType},
					"query": "*",
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

		if res.StatusCode >= http.StatusBadRequest {
			res.Body.Close()
			return nil, fmt.Errorf("unexpected HTTP %d from DataHub searchAcrossEntities", res.StatusCode)
		}

		var gqlResp searchAcrossEntitiesResponse
		decodeErr := json.NewDecoder(res.Body).Decode(&gqlResp)
		res.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("parsing searchAcrossEntities response: %w", decodeErr)
		}
		if len(gqlResp.Errors) > 0 {
			return nil, fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
		}

		page := gqlResp.Data.SearchAcrossEntities.SearchResults
		for _, r := range page {
			if r.Entity.URN != "" {
				urns = append(urns, r.Entity.URN)
			}
		}

		start += len(page)
		if start >= gqlResp.Data.SearchAcrossEntities.Total || len(page) == 0 {
			break
		}
	}

	return urns, nil
}
