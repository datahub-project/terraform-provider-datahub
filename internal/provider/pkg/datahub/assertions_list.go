// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ListAssertionURNs returns the URNs of all DataHub assertions visible to the
// authenticated principal, regardless of assertion type.
//
// Uses searchAcrossEntities with entity type ASSERTION. The search index is
// backed by OpenSearch and is eventually consistent. Assertions created within
// the last few seconds may not appear. This function is intended for
// enumeration (inventory data sources), not for authoritative reads -- use
// GetAssertionByURN for those.
//
// NOTE: this returns ALL assertion types (FRESHNESS, VOLUME, SQL, DATASET,
// FIELD, CUSTOM, ...). It is NOT suitable for driving bulk import of
// datahub_custom_assertion, which models only the CUSTOM type -- use
// ListCustomAssertionURNs for that.
func (c *Client) ListAssertionURNs(ctx context.Context) ([]string, error) {
	return listURNsByEntityType(ctx, c, "ASSERTION")
}

// ListCustomAssertionURNs returns the URNs of only CUSTOM-type assertions.
//
// The DataHub `assertion` entity type is shared by every assertion variant
// (freshness/volume/sql monitors, native DATASET/FIELD assertions, and external
// CUSTOM assertions). The datahub_custom_assertion resource only models the
// CUSTOM type, so bulk import must enumerate by type -- importing a monitor or
// native assertion as a custom assertion would fail or corrupt state. The
// assertion type is read from `info.type` and filtered here.
func (c *Client) ListCustomAssertionURNs(ctx context.Context) ([]string, error) {
	const q = `
query searchAssertions($input: SearchAcrossEntitiesInput!) {
  searchAcrossEntities(input: $input) {
    total
    searchResults {
      entity {
        urn
        ... on Assertion { info { type } }
      }
    }
  }
}`

	type resp struct {
		Data struct {
			SearchAcrossEntities struct {
				Total         int `json:"total"`
				SearchResults []struct {
					Entity struct {
						URN  string `json:"urn"`
						Info *struct {
							Type string `json:"type"`
						} `json:"info"`
					} `json:"entity"`
				} `json:"searchResults"`
			} `json:"searchAcrossEntities"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	const pageSize = 100
	var urns []string
	start := 0

	for {
		body := map[string]any{
			"query": q,
			"variables": map[string]any{
				"input": map[string]any{
					"types": []string{"ASSERTION"},
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

		var gqlResp resp
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
			if r.Entity.URN != "" && r.Entity.Info != nil && r.Entity.Info.Type == "CUSTOM" {
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
