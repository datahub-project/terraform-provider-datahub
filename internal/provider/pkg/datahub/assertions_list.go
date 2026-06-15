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
// authenticated principal, regardless of assertion type or source.
//
// Uses searchAcrossEntities with entity type ASSERTION. The search index is
// backed by OpenSearch and is eventually consistent. Assertions created within
// the last few seconds may not appear. This function is intended for
// enumeration (inventory data sources), not for authoritative reads -- use
// GetAssertionByURN for those.
//
// NOTE: this returns ALL assertion types and sources (including EXTERNAL
// assertions reported by ingestion such as dbt tests, and INFERRED smart/AI
// assertions). It is NOT suitable for driving bulk import of a specific
// assertion resource -- use the type-specific List*AssertionURNs functions,
// which filter to the assertion type, source, and sub-shape each resource models.
func (c *Client) ListAssertionURNs(ctx context.Context) ([]string, error) {
	return listURNsByEntityType(ctx, c, "ASSERTION")
}

// assertionSearchEntity is the per-result shape returned by scanAssertions,
// carrying enough of assertionInfo to route an assertion URN to the Terraform
// resource that models it: the source (NATIVE / EXTERNAL / INFERRED), the
// top-level type, and the sub-shape discriminator.
type assertionSearchEntity struct {
	URN    string
	Source string // NATIVE, EXTERNAL, INFERRED
	Type   string // FRESHNESS, VOLUME, SQL, DATASET, FIELD, DATA_SCHEMA, CUSTOM
	// The following are "" unless the matching nested object is present.
	VolumeSubType         string // ROW_COUNT_TOTAL, ROW_COUNT_CHANGE
	SQLSubType            string // METRIC, METRIC_CHANGE
	FreshnessScheduleType string // CRON, FIXED_INTERVAL, SINCE_THE_LAST_CHECK
}

// scanAssertions pages the ASSERTION search index and returns the URNs of
// entities for which keep returns true. The selection fetches the source and
// the sub-shape discriminators so callers can filter precisely.
//
// Eventual-consistency and "enumeration not authoritative read" caveats from
// ListAssertionURNs apply equally here.
func (c *Client) scanAssertions(ctx context.Context, keep func(assertionSearchEntity) bool) ([]string, error) {
	const q = `
query searchAssertions($input: SearchAcrossEntitiesInput!) {
  searchAcrossEntities(input: $input) {
    total
    searchResults {
      entity {
        urn
        ... on Assertion {
          info {
            type
            source { type }
            volumeAssertion { type }
            sqlAssertion { type }
            freshnessAssertion { schedule { type } }
          }
        }
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
							Type   string `json:"type"`
							Source *struct {
								Type string `json:"type"`
							} `json:"source"`
							VolumeAssertion *struct {
								Type string `json:"type"`
							} `json:"volumeAssertion"`
							SQLAssertion *struct {
								Type string `json:"type"`
							} `json:"sqlAssertion"`
							FreshnessAssertion *struct {
								Schedule *struct {
									Type string `json:"type"`
								} `json:"schedule"`
							} `json:"freshnessAssertion"`
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
			if r.Entity.URN == "" || r.Entity.Info == nil {
				continue
			}
			e := assertionSearchEntity{URN: r.Entity.URN, Type: r.Entity.Info.Type}
			if s := r.Entity.Info.Source; s != nil {
				e.Source = s.Type
			}
			if v := r.Entity.Info.VolumeAssertion; v != nil {
				e.VolumeSubType = v.Type
			}
			if s := r.Entity.Info.SQLAssertion; s != nil {
				e.SQLSubType = s.Type
			}
			if f := r.Entity.Info.FreshnessAssertion; f != nil && f.Schedule != nil {
				e.FreshnessScheduleType = f.Schedule.Type
			}
			if keep(e) {
				urns = append(urns, e.URN)
			}
		}

		start += len(page)
		if start >= gqlResp.Data.SearchAcrossEntities.Total || len(page) == 0 {
			break
		}
	}

	return urns, nil
}

// ListCustomAssertionURNs returns the URNs of CUSTOM-type assertions.
//
// datahub_custom_assertion models externally-evaluated assertions (a third
// party runs the logic and reports results), so it is filtered by type==CUSTOM
// only -- deliberately NOT by source. Unlike the monitor types below, a custom
// assertion is external by design, so a NATIVE filter would wrongly exclude
// every custom assertion. The type==CUSTOM filter already excludes ingested
// dbt/GE tests, which are DATASET/FIELD types.
func (c *Client) ListCustomAssertionURNs(ctx context.Context) ([]string, error) {
	return c.scanAssertions(ctx, func(e assertionSearchEntity) bool {
		return e.Type == "CUSTOM"
	})
}

// ListFreshnessAssertionURNs returns the URNs of freshness assertions that
// datahub_freshness_assertion can manage: NATIVE source (DataHub-run monitors,
// not ingested/auto assertions) with a schedule type the resource models
// (CRON, FIXED_INTERVAL, or SINCE_THE_LAST_CHECK).
func (c *Client) ListFreshnessAssertionURNs(ctx context.Context) ([]string, error) {
	return c.scanAssertions(ctx, func(e assertionSearchEntity) bool {
		return e.Type == "FRESHNESS" && e.Source == "NATIVE" &&
			(e.FreshnessScheduleType == "CRON" || e.FreshnessScheduleType == "FIXED_INTERVAL" ||
				e.FreshnessScheduleType == "SINCE_THE_LAST_CHECK")
	})
}

// ListVolumeAssertionURNs returns the URNs of volume assertions that
// datahub_volume_assertion can manage: NATIVE source with a sub-type the
// resource models (ROW_COUNT_TOTAL or ROW_COUNT_CHANGE).
func (c *Client) ListVolumeAssertionURNs(ctx context.Context) ([]string, error) {
	return c.scanAssertions(ctx, func(e assertionSearchEntity) bool {
		return e.Type == "VOLUME" && e.Source == "NATIVE" &&
			(e.VolumeSubType == "ROW_COUNT_TOTAL" || e.VolumeSubType == "ROW_COUNT_CHANGE")
	})
}

// ListFieldAssertionURNs returns the URNs of NATIVE field (column) assertions.
// Both sub-shapes (FIELD_VALUES, FIELD_METRIC) are managed by the one
// datahub_field_assertion resource, so no sub-shape discriminator is needed.
func (c *Client) ListFieldAssertionURNs(ctx context.Context) ([]string, error) {
	return c.scanAssertions(ctx, func(e assertionSearchEntity) bool {
		return e.Type == "FIELD" && e.Source == "NATIVE"
	})
}

// ListSchemaAssertionURNs returns the URNs of NATIVE schema assertions
// (AssertionType DATA_SCHEMA).
func (c *Client) ListSchemaAssertionURNs(ctx context.Context) ([]string, error) {
	return c.scanAssertions(ctx, func(e assertionSearchEntity) bool {
		return e.Type == "DATA_SCHEMA" && e.Source == "NATIVE"
	})
}

// ListSQLAssertionURNs returns the URNs of SQL assertions that
// datahub_sql_assertion can manage: NATIVE source with a sub-type the resource
// models (METRIC or METRIC_CHANGE).
func (c *Client) ListSQLAssertionURNs(ctx context.Context) ([]string, error) {
	return c.scanAssertions(ctx, func(e assertionSearchEntity) bool {
		return e.Type == "SQL" && e.Source == "NATIVE" &&
			(e.SQLSubType == "METRIC" || e.SQLSubType == "METRIC_CHANGE")
	})
}
