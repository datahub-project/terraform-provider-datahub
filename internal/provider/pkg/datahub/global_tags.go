// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// globalTagsEntity is the OpenAPI v3 response shape for the globalTags aspect
// on any entity GET.
type globalTagsEntity struct {
	URN        string `json:"urn"`
	GlobalTags *struct {
		Value struct {
			Tags []struct {
				Tag string `json:"tag"`
			} `json:"tags"`
		} `json:"value"`
	} `json:"globalTags,omitempty"`
}

// GetGlobalTags reads the globalTags aspect of an entity via the OpenAPI v3
// entity endpoint (MySQL, strongly consistent). entityPath is the lowercase
// path segment for the entity type (e.g. "corpuser", "corpgroup",
// "dataproduct"). Returns the sorted tag URNs, whether the entity exists, and
// an error. An existing entity without the aspect returns an empty list.
func (c *Client) GetGlobalTags(ctx context.Context, entityPath, urn string) ([]string, bool, error) {
	if c == nil {
		return nil, false, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if entityPath == "" || urn == "" {
		return nil, false, errors.New("entityPath and urn are required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/%s/%s", entityPath, urn)
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, false, err
	}

	res, err := c.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, false, fmt.Errorf("unexpected HTTP %d reading globalTags for %s: %s", res.StatusCode, urn, respBody)
	}

	var entity globalTagsEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, false, fmt.Errorf("parsing globalTags response: %w", err)
	}

	var tags []string
	if entity.GlobalTags != nil {
		for _, t := range entity.GlobalTags.Value.Tags {
			if t.Tag != "" {
				tags = append(tags, t.Tag)
			}
		}
	}
	sort.Strings(tags)
	return tags, true, nil
}

// SetGlobalTags writes the complete globalTags aspect of an entity via the
// OpenAPI v3 entity endpoint: the entity carries exactly tagURNs afterwards,
// and an empty list clears the aspect. The write is verified with a read-back
// (DataHub returns HTTP 200 and silently drops aspect writes for entity types
// that do not register the aspect - CAT-2562), so a silent no-op surfaces as
// an error instead of persisting phantom state.
func (c *Client) SetGlobalTags(ctx context.Context, entityPath, urn string, tagURNs []string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if entityPath == "" || urn == "" {
		return errors.New("entityPath and urn are required")
	}

	want := append([]string(nil), tagURNs...)
	sort.Strings(want)

	tags := make([]map[string]any, 0, len(want))
	for _, t := range want {
		tags = append(tags, map[string]any{"tag": t})
	}
	payload := []map[string]any{
		{
			"urn": urn,
			"globalTags": map[string]any{
				"value": map[string]any{"tags": tags},
			},
		},
	}

	path := fmt.Sprintf("/openapi/v3/entity/%s?async=false", entityPath)
	req, err := c.NewRequest(ctx, http.MethodPost, path, payload)
	if err != nil {
		return err
	}

	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("unexpected HTTP %d writing globalTags for %s: %s", res.StatusCode, urn, respBody)
	}

	got, found, err := c.GetGlobalTags(ctx, entityPath, urn)
	if err != nil {
		return fmt.Errorf("verifying globalTags write for %s: %w", urn, err)
	}
	if !found {
		return fmt.Errorf("verifying globalTags write for %s: entity not found on read-back", urn)
	}
	if !stringSlicesEqual(got, want) {
		return fmt.Errorf(
			"DataHub accepted the globalTags write for %s but the aspect did not persist (got %v, want %v); "+
				"the server may not register globalTags for this entity type (CAT-2562-style silent no-op)",
			urn, got, want)
	}
	return nil
}

// stringSlicesEqual compares two sorted string slices, treating nil and empty
// as equal.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
