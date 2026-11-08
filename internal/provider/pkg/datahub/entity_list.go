// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// entityListPageSize is the page size for listEntityURNs. Each page is one
// scroll request; the aspect payloads it carries are small.
const entityListPageSize = 100

// entityListMaxPages bounds listEntityURNs so a server that keeps returning a
// scrollId cannot loop forever. At entityListPageSize per page this is far
// beyond any real estate of ingestion sources or secrets.
const entityListMaxPages = 1000

type entityListPage struct {
	ScrollID string                       `json:"scrollId"`
	Entities []map[string]json.RawMessage `json:"entities"`
}

// listEntityURNs returns the URNs of every entity of one type, by scrolling
// GET /openapi/v3/entity/{entityPath}?query=*&skipCache=true.
//
// skipCache is the point. DataHub can cache search results (DataHub Cloud
// does), keyed by the exact query, filters and sort, for up to ten minutes,
// and a write does not evict them. A list that asks the same question every
// time, as an enumeration does, can therefore be answered from a stale cache
// long after the index itself is current: on DataHub Cloud a new ingestion
// source stayed missing from listIngestionSources for close to three minutes
// while this listing showed it within seconds. The GraphQL list* queries offer
// no way to skip the cache; this endpoint does (DataHub v0.14.0 and later).
//
// The result is still eventually consistent, because the search index itself
// trails writes by a few seconds, so it remains an enumeration and never a
// substitute for the per-entity GET that Read uses.
//
// aspect names the one aspect to fetch. The server omits an entity that lacks
// it, which drops skeleton entities (indexed, but whose create never wrote the
// aspect) the same way the GraphQL list resolvers do. keep, when non-nil,
// filters on that aspect's JSON ({"value":{...}}).
func (c *Client) listEntityURNs(ctx context.Context, entityPath, aspect string, keep func(aspect json.RawMessage) (bool, error)) ([]string, error) {
	var urns []string
	scrollID := ""
	for page := 0; page < entityListMaxPages; page++ {
		params := url.Values{}
		params.Set("query", "*")
		params.Set("count", fmt.Sprint(entityListPageSize))
		params.Set("aspects", aspect)
		params.Set("skipCache", "true")
		if scrollID != "" {
			params.Set("scrollId", scrollID)
		}
		req, err := c.NewRequest(ctx, http.MethodGet, "/openapi/v3/entity/"+entityPath+"?"+params.Encode(), nil)
		if err != nil {
			return nil, err
		}
		res, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("reading %s list response: %w", entityPath, readErr)
		}
		if res.StatusCode >= http.StatusBadRequest {
			return nil, fmt.Errorf("unexpected HTTP %d listing %s", res.StatusCode, entityPath)
		}

		var p entityListPage
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, fmt.Errorf("parsing %s list response: %w", entityPath, err)
		}
		for _, e := range p.Entities {
			var urn string
			if err := json.Unmarshal(e["urn"], &urn); err != nil || urn == "" {
				continue
			}
			raw, ok := e[aspect]
			if !ok {
				continue
			}
			if keep != nil {
				ok, err := keep(raw)
				if err != nil {
					return nil, fmt.Errorf("parsing %s of %s: %w", aspect, urn, err)
				}
				if !ok {
					continue
				}
			}
			urns = append(urns, urn)
		}
		if p.ScrollID == "" || len(p.Entities) == 0 {
			return urns, nil
		}
		scrollID = p.ScrollID
	}
	return nil, fmt.Errorf("listing %s: still paging after %d pages", entityPath, entityListMaxPages)
}
