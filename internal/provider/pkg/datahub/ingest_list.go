// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type listIngestionSourcesResponse struct {
	Data struct {
		ListIngestionSources struct {
			Total            int `json:"total"`
			IngestionSources []struct {
				URN string `json:"urn"`
			} `json:"ingestionSources"`
		} `json:"listIngestionSources"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ListIngestionSourceURNs returns the URNs of all ingestion sources in DataHub.
//
// The underlying listIngestionSources GraphQL query is backed by OpenSearch and
// is eventually consistent. Entities created within the last few seconds may not
// appear. This function is intended for enumeration (extract tooling, inventory
// data sources), not for authoritative reads -- use GetIngestionSourceByID for
// those.
func (c *Client) ListIngestionSourceURNs(ctx context.Context) ([]string, error) {
	const q = `
query listIngestionSources($input: ListIngestionSourcesInput!) {
  listIngestionSources(input: $input) {
    total
    ingestionSources {
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

		if res.StatusCode >= http.StatusBadRequest {
			res.Body.Close()
			return nil, fmt.Errorf("unexpected HTTP %d from DataHub listIngestionSources", res.StatusCode)
		}

		var gqlResp listIngestionSourcesResponse
		decodeErr := json.NewDecoder(res.Body).Decode(&gqlResp)
		res.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("parsing listIngestionSources response: %w", decodeErr)
		}
		if len(gqlResp.Errors) > 0 {
			return nil, fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
		}

		page := gqlResp.Data.ListIngestionSources.IngestionSources
		for _, s := range page {
			urns = append(urns, s.URN)
		}

		start += len(page)
		if start >= gqlResp.Data.ListIngestionSources.Total || len(page) == 0 {
			break
		}
	}

	return urns, nil
}
