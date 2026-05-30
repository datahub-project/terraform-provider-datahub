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
	"strings"
)

// Role is the read-shape returned by GetRoleByURN. DataHub roles (Admin,
// Editor, Reader) are built-in and not creatable; this type backs read-only
// data sources.
type Role struct {
	URN         string
	Name        string
	Description string
	Editable    bool
}

type dataHubRoleEntity struct {
	URN  string `json:"urn"`
	Info *struct {
		Value struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Editable    bool   `json:"editable"`
		} `json:"value"`
	} `json:"dataHubRoleInfo,omitempty"`
}

// GetRoleByURN fetches a DataHub role by URN via the OpenAPI v3 entity endpoint.
// Returns nil (no error) on 404.
func (c *Client) GetRoleByURN(ctx context.Context, urn string) (*Role, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/datahubrole/%s", urn)
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub datahubrole API: %s", res.StatusCode, respBody)
	}

	var entity dataHubRoleEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing datahubrole entity response: %w", err)
	}
	if entity.Info == nil {
		return nil, nil
	}

	return &Role{
		URN:         entity.URN,
		Name:        entity.Info.Value.Name,
		Description: entity.Info.Value.Description,
		Editable:    entity.Info.Value.Editable,
	}, nil
}

type listRolesPageResponse struct {
	Data struct {
		ListRoles struct {
			Total int `json:"total"`
			Roles []struct {
				URN string `json:"urn"`
			} `json:"roles"`
		} `json:"listRoles"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ListRoleURNs returns the URNs of all DataHub roles. The built-in set is small
// (Admin, Editor, Reader), but pagination is implemented for completeness.
func (c *Client) ListRoleURNs(ctx context.Context) ([]string, error) {
	const q = `
query listRoles($input: ListRolesInput!) {
  listRoles(input: $input) {
    total
    roles {
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

		if res.StatusCode >= http.StatusBadRequest {
			res.Body.Close()
			return nil, fmt.Errorf("unexpected HTTP %d from DataHub listRoles", res.StatusCode)
		}

		var gqlResp listRolesPageResponse
		decodeErr := json.NewDecoder(res.Body).Decode(&gqlResp)
		res.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("parsing listRoles response: %w", decodeErr)
		}
		if len(gqlResp.Errors) > 0 {
			return nil, fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
		}

		page := gqlResp.Data.ListRoles.Roles
		for _, r := range page {
			urns = append(urns, r.URN)
		}

		start += len(page)
		if start >= gqlResp.Data.ListRoles.Total || len(page) == 0 {
			break
		}
	}

	return urns, nil
}
