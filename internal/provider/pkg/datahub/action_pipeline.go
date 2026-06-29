// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// Action pipeline (a.k.a. automation) management for DataHub Cloud.
//
// An action pipeline is the `dataHubAction` entity: a packaged action that
// runs a recipe (e.g. propagating descriptions/tags/glossary terms back to
// BigQuery or Dataplex). It is Cloud-only -- the entity type and the
// upsert/delete/list mutations are absent from OSS DataHub.
//
// Writes go through the `upsertActionPipeline` GraphQL mutation (which also
// reloads/starts the action runtime); reads use the strongly-consistent
// OpenAPI v3 entity endpoint (GET /openapi/v3/entity/datahubaction/{urn}).
//
// The URN is `urn:li:dataHubAction:<id>`. `createActionPipeline` mints a random
// UUID, but `upsertActionPipeline(urn, input)` creates at a caller-chosen URN
// (verified live), so the resource derives a deterministic id client-side and
// always upserts -- never calls createActionPipeline.

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

// ErrActionPipelineCloudOnly is returned when an action pipeline mutation or
// query is attempted against an OSS DataHub instance that does not expose the
// dataHubAction entity.
var ErrActionPipelineCloudOnly = errors.New(
	"action pipelines require DataHub Cloud; " +
		"the configured GMS instance does not expose action pipeline management",
)

// ActionPipelineURNPrefix is the URN namespace for action pipelines.
const ActionPipelineURNPrefix = "urn:li:dataHubAction:"

// isActionPipelineCloudOnlyError returns true when the GraphQL error indicates
// the mutation/query or the dataHubAction type is absent from the OSS schema.
func isActionPipelineCloudOnlyError(msg string) bool {
	if strings.Contains(msg, "FieldUndefined") &&
		(strings.Contains(msg, "in type 'Mutation'") || strings.Contains(msg, "in type 'Query'")) {
		return true
	}
	if strings.Contains(msg, "UnknownType") && strings.Contains(msg, "ActionPipeline") {
		return true
	}
	return false
}

// ActionPipelineInput groups the inputs for upsertActionPipeline.
type ActionPipelineInput struct {
	ActionID    string // URN key; the resource derives this deterministically
	Name        string
	Type        string // free-form action class
	Category    string // optional
	Description string // optional
	Recipe      string // JSON document
	ExecutorID  string // optional
	Version     string // optional
	DebugMode   *bool  // optional
}

// ActionPipelineInfo is the read shape from the OpenAPI v3 entity endpoint.
type ActionPipelineInfo struct {
	ID          string
	Name        string
	Type        string
	Category    string
	Description string
	Recipe      string
	ExecutorID  string
	Version     string
	DebugMode   *bool
}

// actionPipelineEntity is the OpenAPI v3 response shape for
// GET /openapi/v3/entity/datahubaction/{urn}.
type actionPipelineEntity struct {
	URN string `json:"urn"`
	Key *struct {
		Value struct {
			ID string `json:"id"`
		} `json:"value"`
	} `json:"dataHubActionKey,omitempty"`
	Info *struct {
		Value struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Type        string `json:"type"`
			Category    string `json:"category"`
			Config      struct {
				Recipe     string `json:"recipe"`
				ExecutorID string `json:"executorId"`
				Version    string `json:"version"`
				DebugMode  *bool  `json:"debugMode"`
			} `json:"config"`
		} `json:"value"`
	} `json:"dataHubActionInfo,omitempty"`
}

// UpsertActionPipeline creates or updates an action pipeline at the deterministic
// URN urn:li:dataHubAction:<ActionID>. It returns that URN. Requires DataHub
// Cloud; returns ErrActionPipelineCloudOnly on OSS.
//
// Note: a non-nil error may still leave the definition persisted -- the upsert
// writes the metadata and then reloads/starts the action runtime, and the
// reload can fail (HTTP 500 "Failed to reload ...") for a recipe that cannot
// run while the dataHubActionInfo aspect is already written. The returned URN is
// always the deterministic one so the caller can verify via GetActionPipelineByID.
func (c *Client) UpsertActionPipeline(ctx context.Context, in ActionPipelineInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}
	in.ActionID = strings.TrimSpace(in.ActionID)
	if in.ActionID == "" {
		return "", errors.New("action ID is required")
	}

	const q = `
mutation upsertActionPipeline($urn: String!, $input: UpdateActionPipelineInput!) {
  upsertActionPipeline(urn: $urn, input: $input)
}`

	config := map[string]any{"recipe": in.Recipe}
	if in.ExecutorID != "" {
		config["executorId"] = in.ExecutorID
	}
	if in.Version != "" {
		config["version"] = in.Version
	}
	if in.DebugMode != nil {
		config["debugMode"] = *in.DebugMode
	}
	input := map[string]any{
		"name":   in.Name,
		"type":   in.Type,
		"config": config,
	}
	if in.Category != "" {
		input["category"] = in.Category
	}
	if in.Description != "" {
		input["description"] = in.Description
	}

	urn := ActionPipelineURNPrefix + in.ActionID
	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"urn": urn, "input": input},
	}

	var raw struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.doGraphQL(ctx, body, &raw); err != nil {
		return urn, err
	}
	if len(raw.Errors) > 0 {
		msg := raw.Errors[0].Message
		if isActionPipelineCloudOnlyError(msg) {
			return "", ErrActionPipelineCloudOnly
		}
		return urn, fmt.Errorf("DataHub API error: %s", msg)
	}
	return urn, nil
}

// GetActionPipelineByID reads an action pipeline from the OpenAPI v3 entity
// endpoint (MySQL, strongly consistent). Returns nil (no error) on 404.
func (c *Client) GetActionPipelineByID(ctx context.Context, actionID string) (*ActionPipelineInfo, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return nil, errors.New("action ID is required")
	}

	urn := ActionPipelineURNPrefix + actionID
	path := fmt.Sprintf("/openapi/v3/entity/datahubaction/%s", urn)
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
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub action pipeline API: %s", res.StatusCode, respBody)
	}

	var entity actionPipelineEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing action pipeline entity response: %w", err)
	}
	if entity.Key == nil && entity.Info == nil {
		return nil, nil
	}

	out := &ActionPipelineInfo{ID: actionID}
	if entity.Key != nil && entity.Key.Value.ID != "" {
		out.ID = entity.Key.Value.ID
	}
	if entity.Info != nil {
		v := entity.Info.Value
		out.Name = v.Name
		out.Type = v.Type
		out.Category = v.Category
		out.Description = v.Description
		out.Recipe = v.Config.Recipe
		out.ExecutorID = v.Config.ExecutorID
		out.Version = v.Config.Version
		out.DebugMode = v.Config.DebugMode
	}
	return out, nil
}

// DeleteActionPipeline hard-deletes an action pipeline by id via the
// deleteActionPipeline GraphQL mutation. A not-found result is treated as
// success (the entity is already gone).
func (c *Client) DeleteActionPipeline(ctx context.Context, actionID string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return errors.New("action ID is required")
	}

	const q = `
mutation deleteActionPipeline($urn: String!) {
  deleteActionPipeline(urn: $urn)
}`
	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"urn": ActionPipelineURNPrefix + actionID},
	}
	var raw genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &raw); err != nil {
		return err
	}
	if len(raw.Errors) > 0 {
		msg := raw.Errors[0].Message
		if strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist") {
			return nil
		}
		if isActionPipelineCloudOnlyError(msg) {
			return ErrActionPipelineCloudOnly
		}
		return fmt.Errorf("DataHub API error: %s", msg)
	}
	return nil
}

// ListActionPipelineURNs returns the URNs of all action pipelines visible to
// the authenticated principal, via the listActionPipelines GraphQL query.
// Returns ErrActionPipelineCloudOnly on OSS. Backed by search (eventually
// consistent) -- for enumeration/import, not authoritative reads.
func (c *Client) ListActionPipelineURNs(ctx context.Context) ([]string, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}

	const q = `
query listActionPipelines($input: ListActionPipelinesInput!) {
  listActionPipelines(input: $input) {
    total
    actionPipelines { urn }
  }
}`

	const pageSize = 100
	var urns []string
	start := 0
	for {
		body := map[string]any{
			"query": q,
			"variables": map[string]any{
				"input": map[string]any{"start": start, "count": pageSize},
			},
		}
		var raw struct {
			Data struct {
				ListActionPipelines struct {
					Total           int `json:"total"`
					ActionPipelines []struct {
						URN string `json:"urn"`
					} `json:"actionPipelines"`
				} `json:"listActionPipelines"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := c.doGraphQL(ctx, body, &raw); err != nil {
			return nil, err
		}
		if len(raw.Errors) > 0 {
			msg := raw.Errors[0].Message
			if isActionPipelineCloudOnlyError(msg) {
				return nil, ErrActionPipelineCloudOnly
			}
			return nil, fmt.Errorf("DataHub API error: %s", msg)
		}
		page := raw.Data.ListActionPipelines.ActionPipelines
		for _, p := range page {
			if p.URN != "" {
				urns = append(urns, p.URN)
			}
		}
		start += len(page)
		if start >= raw.Data.ListActionPipelines.Total || len(page) == 0 {
			break
		}
	}
	return urns, nil
}
