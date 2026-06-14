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
				URN    string `json:"urn"`
				Source *struct {
					Type string `json:"type"`
				} `json:"source"`
			} `json:"ingestionSources"`
		} `json:"listIngestionSources"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ListIngestionSourceURNs returns the URNs of all user-managed ingestion sources
// in DataHub.
//
// DataHub Cloud provisions internal "system" ingestion sources (datahub-gc,
// datahub-usage-reporting, semantic-anchor, user-entity-resolution, ...) that
// run platform maintenance jobs. They are owned by urn:li:corpuser:__datahub_system
// and must never be brought under Terraform management -- the datahub_ingestion_source
// resource cannot meaningfully own them and an apply would fight the platform.
// They are identified by source.type == "SYSTEM" in the listIngestionSources
// response and are filtered out here, so both the extract CLI and the
// datahub_ingestion_sources data source only surface user-managed sources.
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
      source {
        type
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
			// Skip platform-internal system sources (see doc comment); pagination
			// still advances by the full page length below since `total` counts them.
			if s.Source != nil && s.Source.Type == "SYSTEM" {
				continue
			}
			urns = append(urns, s.URN)
		}

		start += len(page)
		if start >= gqlResp.Data.ListIngestionSources.Total || len(page) == 0 {
			break
		}
	}

	return urns, nil
}
