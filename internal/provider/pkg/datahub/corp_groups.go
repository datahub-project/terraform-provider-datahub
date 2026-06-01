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

// CorpGroup is the read-shape returned by GetGroupByURN.
type CorpGroup struct {
	URN         string
	ID          string
	Name        string // displayName
	Description string
	Email       string
	Slack       string
}

// CreateGroupInput groups the inputs for creating a native DataHub group.
type CreateGroupInput struct {
	// ID becomes the URN suffix: urn:li:corpGroup:<ID>. Required.
	ID   string
	Name string // displayName
}

// UpdateGroupPropsInput carries the editable group properties written to the
// corpGroupEditableInfo aspect via updateCorpGroupProperties.
type UpdateGroupPropsInput struct {
	URN         string
	Description string
	Email       string
	Slack       string
}

// corpGroupEntity is the OpenAPI v3 response shape for
// GET /openapi/v3/entity/corpgroup/{urn}.
type corpGroupEntity struct {
	URN     string `json:"urn"`
	KeyData *struct {
		Value struct {
			Name string `json:"name"`
		} `json:"value"`
	} `json:"corpGroupKey,omitempty"`
	Info *struct {
		Value struct {
			DisplayName string `json:"displayName"`
			Description string `json:"description"`
		} `json:"value"`
	} `json:"corpGroupInfo,omitempty"`
	EditableInfo *struct {
		Value struct {
			Description string `json:"description"`
			Email       string `json:"email"`
			Slack       string `json:"slack"`
		} `json:"value"`
	} `json:"corpGroupEditableInfo,omitempty"`
}

type createGroupResponse struct {
	Data struct {
		CreateGroup string `json:"createGroup"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type genericGraphQLErrors struct {
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// CreateGroup creates a native DataHub group via the GraphQL API and returns
// the group URN. createGroup is create-only: calling it for an existing id
// returns an error ("This Group already exists!"). Display-name and property
// updates use UpdateGroupName and UpdateGroupProperties.
func (c *Client) CreateGroup(ctx context.Context, in CreateGroupInput) (string, error) {
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
mutation createGroup($input: CreateGroupInput!) {
  createGroup(input: $input)
}`

	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"id":   in.ID,
				"name": in.Name,
			},
		},
	}

	var gqlResp createGroupResponse
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return "", err
	}
	if len(gqlResp.Errors) > 0 {
		return "", fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}

	urn := gqlResp.Data.CreateGroup
	if urn == "" {
		urn = "urn:li:corpGroup:" + in.ID
	}
	return urn, nil
}

// UpdateGroupName updates a group's display name via the updateName mutation,
// which writes corpGroupInfo.displayName.
func (c *Client) UpdateGroupName(ctx context.Context, urn, name string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	const q = `
mutation updateName($input: UpdateNameInput!) {
  updateName(input: $input)
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"urn":  urn,
				"name": strings.TrimSpace(name),
			},
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

// UpdateGroupProperties writes the editable group properties (description,
// email, slack) to the corpGroupEditableInfo aspect via
// updateCorpGroupProperties. Empty strings clear the corresponding field.
func (c *Client) UpdateGroupProperties(ctx context.Context, in UpdateGroupPropsInput) error {
	if c == nil {
		return errors.New("client is nil")
	}
	const q = `
mutation updateCorpGroupProperties($urn: String!, $input: CorpGroupUpdateInput!) {
  updateCorpGroupProperties(urn: $urn, input: $input) {
    urn
  }
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"urn": in.URN,
			"input": map[string]any{
				"description": in.Description,
				"email":       in.Email,
				"slack":       in.Slack,
			},
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

// GetGroupByURN fetches a DataHub group directly by URN via the OpenAPI v3
// entity endpoint (MySQL, strongly consistent). Returns nil (no error) on 404.
//
// editableInfo.description (UI-edited) takes precedence over
// corpGroupInfo.description, matching the DataHub UI.
func (c *Client) GetGroupByURN(ctx context.Context, urn string) (*CorpGroup, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/corpgroup/%s", urn)
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
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_USERS_AND_GROUPS privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub corpgroup API: %s", res.StatusCode, respBody)
	}

	var entity corpGroupEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing corpgroup entity response: %w", err)
	}

	if entity.Info == nil && entity.KeyData == nil {
		return nil, nil
	}

	id := ""
	if entity.KeyData != nil {
		id = entity.KeyData.Value.Name
	}
	if id == "" {
		id = strings.TrimPrefix(entity.URN, "urn:li:corpGroup:")
	}

	group := &CorpGroup{URN: entity.URN, ID: id}
	if entity.Info != nil {
		group.Name = entity.Info.Value.DisplayName
		group.Description = entity.Info.Value.Description
	}
	if entity.EditableInfo != nil {
		if entity.EditableInfo.Value.Description != "" {
			group.Description = entity.EditableInfo.Value.Description
		}
		group.Email = entity.EditableInfo.Value.Email
		group.Slack = entity.EditableInfo.Value.Slack
	}

	return group, nil
}

// DeleteGroup deletes a DataHub group by URN via the removeGroup GraphQL
// mutation. Returns nil if the group is already gone (idempotent).
func (c *Client) DeleteGroup(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("URN is required")
	}

	const q = `
mutation removeGroup($urn: String!) {
  removeGroup(urn: $urn)
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

// doGraphQL posts a GraphQL request body, decodes the response into out, and
// maps transport-level auth/status failures to errors. GraphQL-level errors[]
// are left in out for the caller to inspect.
func (c *Client) doGraphQL(ctx context.Context, body, out any) error {
	return c.doGraphQLAt(ctx, "/api/graphql", body, out)
}

func (c *Client) doGraphQLAt(ctx context.Context, path string, body, out any) error {
	req, err := c.NewRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return err
	}
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal lacks the required privilege (e.g. MANAGE_USERS_AND_GROUPS)", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("unexpected HTTP %d from DataHub GraphQL API: %s", res.StatusCode, respBody)
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("parsing GraphQL response: %w", err)
	}
	return nil
}
