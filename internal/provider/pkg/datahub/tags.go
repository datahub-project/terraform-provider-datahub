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

// Tag is the read-shape returned by GetTagByURN.
type Tag struct {
	URN         string
	ID          string
	Name        string
	Description string
	ColorHex    string // "#RRGGBB" or ""
}

// CreateTagInput groups the inputs for creating a DataHub tag.
type CreateTagInput struct {
	// ID becomes the URN suffix: urn:li:tag:<ID>. Required. Always supply an
	// explicit value; omitting it causes the DataHub server to generate a random
	// UUID, making the URN non-deterministic and unmanageable by Terraform.
	ID          string
	Name        string
	Description string // optional; omitted when empty
}

// tagEntity is the OpenAPI v3 response shape for
// GET /openapi/v3/entity/tag/{urn}.
type tagEntity struct {
	URN     string `json:"urn"`
	KeyData *struct {
		Value struct {
			Name string `json:"name"`
		} `json:"value"`
	} `json:"tagKey,omitempty"`
	Props *struct {
		Value struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			ColorHex    string `json:"colorHex"`
		} `json:"value"`
	} `json:"tagProperties,omitempty"`
}

type createTagResponse struct {
	Data struct {
		CreateTag string `json:"createTag"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// CreateTag creates a DataHub tag via the GraphQL API and returns its URN.
// Always supply a non-empty ID to produce a deterministic URN; omitting it
// causes the server to generate a random UUID.
func (c *Client) CreateTag(ctx context.Context, in CreateTagInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.ID == "" {
		return "", errors.New("id is required")
	}
	if in.Name == "" {
		return "", errors.New("name is required")
	}

	const q = `
mutation createTag($input: CreateTagInput!) {
  createTag(input: $input)
}`

	input := map[string]any{
		"id":   in.ID,
		"name": in.Name,
	}
	if in.Description != "" {
		input["description"] = in.Description
	}

	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"input": input},
	}

	var gqlResp createTagResponse
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return "", err
	}
	if len(gqlResp.Errors) > 0 {
		return "", fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}

	urn := gqlResp.Data.CreateTag
	if urn == "" {
		urn = "urn:li:tag:" + in.ID
	}
	return urn, nil
}

// GetTagByURN fetches a DataHub tag directly by URN via the OpenAPI v3 entity
// endpoint (MySQL, strongly consistent). Returns nil (no error) on 404.
func (c *Client) GetTagByURN(ctx context.Context, urn string) (*Tag, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/tag/%s", urn)
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
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_TAGS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub tag API: %s", res.StatusCode, respBody)
	}

	var entity tagEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing tag entity response: %w", err)
	}

	if entity.KeyData == nil && entity.Props == nil {
		return nil, nil
	}

	// Derive the ID from the key aspect name, falling back to URN suffix.
	id := ""
	if entity.KeyData != nil {
		id = entity.KeyData.Value.Name
	}
	if id == "" {
		id = strings.TrimPrefix(entity.URN, "urn:li:tag:")
	}

	tag := &Tag{URN: entity.URN, ID: id}
	if entity.Props != nil {
		tag.Name = entity.Props.Value.Name
		tag.Description = entity.Props.Value.Description
		tag.ColorHex = entity.Props.Value.ColorHex
	}
	return tag, nil
}

// SetTagColor sets or updates the colorHex field on an existing tag via the
// dedicated setTagColor GraphQL mutation. The tag must already exist.
// colorHex should be in the form "#RRGGBB".
func (c *Client) SetTagColor(ctx context.Context, urn, colorHex string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	const q = `
mutation setTagColor($urn: String!, $colorHex: String!) {
  setTagColor(urn: $urn, colorHex: $colorHex)
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"urn":      urn,
			"colorHex": colorHex,
		},
	}
	var gqlResp genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return err
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	return nil
}

// WriteTagProperties updates the tagProperties aspect of an existing tag via
// the OpenAPI v3 entity collection endpoint. This is the only available path
// for renaming a tag -- the updateName GraphQL mutation does not support the
// TAG entity type. Pass an empty string for description or colorHex to clear
// those optional fields.
func (c *Client) WriteTagProperties(ctx context.Context, urn, name, description, colorHex string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	name = strings.TrimSpace(name)
	if urn == "" {
		return errors.New("URN is required")
	}
	if name == "" {
		return errors.New("name is required")
	}

	propsValue := map[string]any{
		"name": name,
	}
	if description != "" {
		propsValue["description"] = description
	}
	if colorHex != "" {
		propsValue["colorHex"] = colorHex
	}

	payload := []map[string]any{
		{
			"urn": urn,
			"tagProperties": map[string]any{
				"value": propsValue,
			},
		},
	}

	req, err := c.NewRequest(ctx, http.MethodPost, "/openapi/v3/entity/tag?async=false", payload)
	if err != nil {
		return fmt.Errorf("building tag properties write request: %w", err)
	}

	res, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("tag properties write request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_TAGS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("unexpected HTTP %d from DataHub tag properties write API: %s", res.StatusCode, respBody)
	}
	return nil
}

// DeleteTag hard-deletes a DataHub tag by URN via the deleteTag GraphQL
// mutation. Tags are flat (no children), so no child-guard or retry logic
// is needed.
func (c *Client) DeleteTag(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("URN is required")
	}

	const q = `
mutation deleteTag($urn: String!) {
  deleteTag(urn: $urn)
}`
	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"urn": urn},
	}
	var gqlResp genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return err
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	return nil
}
