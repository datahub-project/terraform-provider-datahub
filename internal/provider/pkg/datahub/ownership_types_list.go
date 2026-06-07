// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type listOwnershipTypesResponse struct {
	Data struct {
		ListOwnershipTypes struct {
			Total          int `json:"total"`
			OwnershipTypes []struct {
				URN string `json:"urn"`
			} `json:"ownershipTypes"`
		} `json:"listOwnershipTypes"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ListOwnershipTypeURNs returns the URNs of all DataHub ownership types visible
// to the authenticated principal, including built-in system types
// (urn:li:ownershipType:__system__*).
//
// Uses the listOwnershipTypes GraphQL query. Note: OWNERSHIP_TYPE is not a
// valid value in the GraphQL EntityType enum, so searchAcrossEntities cannot
// be used for this entity type.
func (c *Client) ListOwnershipTypeURNs(ctx context.Context) ([]string, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}

	const q = `
query listOwnershipTypes($input: ListOwnershipTypesInput!) {
  listOwnershipTypes(input: $input) {
    total
    ownershipTypes { urn }
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

		var resp listOwnershipTypesResponse
		if err := c.doGraphQL(ctx, body, &resp); err != nil {
			return nil, fmt.Errorf("listing ownership types: %w", err)
		}
		if len(resp.Errors) > 0 {
			return nil, fmt.Errorf("DataHub API error listing ownership types: %s", resp.Errors[0].Message)
		}

		page := resp.Data.ListOwnershipTypes
		for _, ot := range page.OwnershipTypes {
			if strings.TrimSpace(ot.URN) != "" {
				urns = append(urns, ot.URN)
			}
		}

		start += len(page.OwnershipTypes)
		if start >= page.Total || len(page.OwnershipTypes) == 0 {
			break
		}
	}

	return urns, nil
}
