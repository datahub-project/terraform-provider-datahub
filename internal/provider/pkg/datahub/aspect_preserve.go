// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// unmanagedAspectFields reads the current value of an aspect and returns it
// with the managed fields removed.
//
// An OpenAPI v3 aspect write replaces the whole stored aspect, so a write that
// sends only the fields the provider manages erases every other field, for
// example a data product's assets or a glossary term's sourceRef. Callers start
// from this map and set their managed fields on it, so fields they do not model
// are written back unchanged. The value is decoded generically to also keep
// fields added to the aspect in later DataHub versions. A missing entity or
// aspect returns an empty map.
func (c *Client) unmanagedAspectFields(ctx context.Context, entityPath, urn, aspectName string, managed []string) (map[string]any, error) {
	path := fmt.Sprintf("/openapi/v3/entity/%s/%s", entityPath, urn)
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("building %s read request: %w", aspectName, err)
	}

	res, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s read request failed: %w", aspectName, err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return map[string]any{}, nil
	}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d) reading %s for %s: the calling principal needs read access to the entity it manages", res.StatusCode, aspectName, urn)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d reading %s for %s: %s", res.StatusCode, aspectName, urn, respBody)
	}

	var entity map[string]json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing %s response: %w", aspectName, err)
	}
	var aspect struct {
		Value map[string]any `json:"value"`
	}
	if raw, ok := entity[aspectName]; ok {
		if err := json.Unmarshal(raw, &aspect); err != nil {
			return nil, fmt.Errorf("parsing %s aspect: %w", aspectName, err)
		}
	}
	if aspect.Value == nil {
		return map[string]any{}, nil
	}
	for _, k := range managed {
		delete(aspect.Value, k)
	}
	return aspect.Value, nil
}
