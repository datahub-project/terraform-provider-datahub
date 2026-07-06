// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// Assertion assignment rule management for DataHub Cloud.
//
// An assertion assignment rule (the `assertionAssignmentRule` entity) is a
// declarative rule that auto-assigns freshness and/or volume monitors to every
// dataset matching a search filter -- far higher leverage than authoring a
// per-asset assertion on each dataset. It is Cloud-only: the entity type and
// the create/update/delete mutations are absent from OSS DataHub, which has no
// monitor service layer.
//
// Writes go through the createAssertionAssignmentRule / updateAssertionAssignmentRule
// GraphQL mutations; reads use the strongly-consistent OpenAPI v3 entity
// endpoint (GET /openapi/v3/entity/assertionassignmentrule/{urn}). Deletes use
// the deleteAssertionAssignmentRule mutation.
//
// The URN is `urn:li:assertionAssignmentRule:<id>`. Create accepts a
// caller-supplied `id` (verified live), so the resource derives a deterministic
// id client-side and the URN is known upfront.
//
// Only freshness and volume monitors are auto-assignable by a rule -- the API
// exposes no sql/field/schema assignment. Subscription config exists on the
// input but is not modeled here.

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

// ErrAssertionAssignmentRuleCloudOnly is returned when an assignment rule
// mutation or query is attempted against an OSS DataHub instance that does not
// expose the assertionAssignmentRule entity.
var ErrAssertionAssignmentRuleCloudOnly = errors.New(
	"assertion assignment rules require DataHub Cloud; " +
		"the configured GMS instance does not expose assertion assignment rule management",
)

// AssertionAssignmentRuleURNPrefix is the URN namespace for assignment rules.
const AssertionAssignmentRuleURNPrefix = "urn:li:assertionAssignmentRule:"

// isAssertionAssignmentRuleCloudOnlyError reports whether a GraphQL error
// indicates the mutation/query or the entity type is absent from the OSS schema.
func isAssertionAssignmentRuleCloudOnlyError(msg string) bool {
	if strings.Contains(msg, "FieldUndefined") &&
		(strings.Contains(msg, "in type 'Mutation'") || strings.Contains(msg, "in type 'Query'")) {
		return true
	}
	if strings.Contains(msg, "UnknownType") && strings.Contains(msg, "AssertionAssignmentRule") {
		return true
	}
	return false
}

// FacetFilter is one predicate in the search filter DSL: field <condition> values.
type FacetFilter struct {
	Field     string
	Values    []string
	Condition string // FilterOperator; empty means EQUAL (the server default)
	Negated   bool
}

// AndFilter is a conjunction of facet filters. A rule's target is a disjunction
// (OR) of these AND-groups.
type AndFilter struct {
	And []FacetFilter
}

// AssertionAssignmentRuleCategoryConfig configures the monitors a rule creates
// for one category (freshness or volume): the evaluation source and the
// incident actions taken on assertion success/failure.
type AssertionAssignmentRuleCategoryConfig struct {
	SourceType       string   // DatasetFreshnessSourceType / DatasetVolumeSourceType; optional
	OnSuccessActions []string // AssertionActionType values (RAISE_INCIDENT / RESOLVE_INCIDENT)
	OnFailureActions []string
}

// AssertionAssignmentRuleInput is the desired state for create/update.
type AssertionAssignmentRuleInput struct {
	ID        string // URN key; the resource derives this deterministically
	Name      string
	Query     string // free-text query, stored as entityFilter.json; "*" matches all
	OrFilters []AndFilter
	Mode      string // ENABLED / DISABLED; applied on update (create defaults ENABLED)
	Freshness *AssertionAssignmentRuleCategoryConfig
	Volume    *AssertionAssignmentRuleCategoryConfig
}

// AssertionAssignmentRuleInfo is the read shape from the OpenAPI v3 endpoint.
type AssertionAssignmentRuleInfo struct {
	ID        string
	Name      string
	Mode      string
	Query     string
	OrFilters []AndFilter
	Freshness *AssertionAssignmentRuleCategoryConfig
	Volume    *AssertionAssignmentRuleCategoryConfig
}

// assertionAssignmentRuleEntity is the OpenAPI v3 response for
// GET /openapi/v3/entity/assertionassignmentrule/{urn}.
type assertionAssignmentRuleEntity struct {
	URN string `json:"urn"`
	Key *struct {
		Value struct {
			ID string `json:"id"`
		} `json:"value"`
	} `json:"assertionAssignmentRuleKey,omitempty"`
	Info *struct {
		Value struct {
			Mode         string `json:"mode"`
			Name         string `json:"name"`
			EntityFilter struct {
				JSON   string `json:"json"`
				Filter struct {
					Or []struct {
						And []struct {
							Field     string   `json:"field"`
							Values    []string `json:"values"`
							Condition string   `json:"condition"`
							Negated   bool     `json:"negated"`
						} `json:"and"`
					} `json:"or"`
				} `json:"filter"`
			} `json:"entityFilter"`
			FreshnessConfig *ruleCategoryConfigJSON `json:"freshnessConfig,omitempty"`
			VolumeConfig    *ruleCategoryConfigJSON `json:"volumeConfig,omitempty"`
		} `json:"value"`
	} `json:"assertionAssignmentRuleInfo,omitempty"`
}

// ruleCategoryConfigJSON is the read shape of freshnessConfig / volumeConfig.
type ruleCategoryConfigJSON struct {
	Enabled                       bool `json:"enabled"`
	PreferredEvaluationParameters *struct {
		SourceType string `json:"sourceType"`
	} `json:"preferredEvaluationParameters,omitempty"`
	OnSuccess []struct {
		Type string `json:"type"`
	} `json:"onSuccess"`
	OnFailure []struct {
		Type string `json:"type"`
	} `json:"onFailure"`
}

// orFiltersToGraphQL converts the client filter model to the GraphQL
// [AndFilterInput!]! shape.
func orFiltersToGraphQL(orFilters []AndFilter) []map[string]any {
	out := make([]map[string]any, 0, len(orFilters))
	for _, group := range orFilters {
		ands := make([]map[string]any, 0, len(group.And))
		for _, f := range group.And {
			facet := map[string]any{
				"field":   f.Field,
				"values":  f.Values,
				"negated": f.Negated,
			}
			if f.Condition != "" {
				facet["condition"] = f.Condition
			}
			ands = append(ands, facet)
		}
		out = append(out, map[string]any{"and": ands})
	}
	return out
}

// categoryConfigToGraphQL converts a category config to the GraphQL
// AssertionAssignmentRule{Freshness,Volume}ConfigInput shape.
func categoryConfigToGraphQL(cfg *AssertionAssignmentRuleCategoryConfig) map[string]any {
	if cfg == nil {
		return nil
	}
	m := map[string]any{}
	if cfg.SourceType != "" {
		m["sourceType"] = cfg.SourceType
	}
	if len(cfg.OnSuccessActions) > 0 {
		m["onSuccess"] = actionsToGraphQL(cfg.OnSuccessActions)
	}
	if len(cfg.OnFailureActions) > 0 {
		m["onFailure"] = actionsToGraphQL(cfg.OnFailureActions)
	}
	return m
}

func actionsToGraphQL(types []string) []map[string]any {
	out := make([]map[string]any, 0, len(types))
	for _, t := range types {
		out = append(out, map[string]any{"type": t})
	}
	return out
}

// CreateAssertionAssignmentRule creates a rule at the deterministic URN
// urn:li:assertionAssignmentRule:<in.ID> and returns that URN. Requires DataHub
// Cloud; returns ErrAssertionAssignmentRuleCloudOnly on OSS.
func (c *Client) CreateAssertionAssignmentRule(ctx context.Context, in AssertionAssignmentRuleInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}
	in.ID = strings.TrimSpace(in.ID)
	if in.ID == "" {
		return "", errors.New("assignment rule ID is required")
	}
	if strings.TrimSpace(in.Name) == "" {
		return "", errors.New("assignment rule name is required")
	}

	const q = `
mutation createAssertionAssignmentRule($input: CreateAssertionAssignmentRuleInput!) {
  createAssertionAssignmentRule(input: $input) { urn }
}`

	input := map[string]any{
		"id":        in.ID,
		"name":      in.Name,
		"filter":    in.Query,
		"orFilters": orFiltersToGraphQL(in.OrFilters),
	}
	if cfg := categoryConfigToGraphQL(in.Freshness); cfg != nil {
		input["freshnessConfig"] = cfg
	}
	if cfg := categoryConfigToGraphQL(in.Volume); cfg != nil {
		input["volumeConfig"] = cfg
	}

	urn := AssertionAssignmentRuleURNPrefix + in.ID
	body := map[string]any{"query": q, "variables": map[string]any{"input": input}}

	var raw genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &raw); err != nil {
		return urn, err
	}
	if len(raw.Errors) > 0 {
		msg := raw.Errors[0].Message
		if isAssertionAssignmentRuleCloudOnlyError(msg) {
			return "", ErrAssertionAssignmentRuleCloudOnly
		}
		return urn, fmt.Errorf("DataHub API error: %s", msg)
	}
	return urn, nil
}

// UpdateAssertionAssignmentRule updates a rule in place. Requires DataHub Cloud.
func (c *Client) UpdateAssertionAssignmentRule(ctx context.Context, urn string, in AssertionAssignmentRuleInput) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("assignment rule URN is required")
	}

	const q = `
mutation updateAssertionAssignmentRule($urn: String!, $input: UpdateAssertionAssignmentRuleInput!) {
  updateAssertionAssignmentRule(urn: $urn, input: $input) { urn }
}`

	input := map[string]any{
		"name":      in.Name,
		"filter":    in.Query,
		"orFilters": orFiltersToGraphQL(in.OrFilters),
	}
	if in.Mode != "" {
		input["mode"] = in.Mode
	}
	// Send config keys even when nil so clearing a category (removing the block)
	// takes effect on update rather than leaving a stale monitor config.
	input["freshnessConfig"] = categoryConfigToGraphQL(in.Freshness)
	input["volumeConfig"] = categoryConfigToGraphQL(in.Volume)

	body := map[string]any{"query": q, "variables": map[string]any{"urn": urn, "input": input}}

	var raw genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &raw); err != nil {
		return err
	}
	if len(raw.Errors) > 0 {
		msg := raw.Errors[0].Message
		if isAssertionAssignmentRuleCloudOnlyError(msg) {
			return ErrAssertionAssignmentRuleCloudOnly
		}
		return fmt.Errorf("DataHub API error: %s", msg)
	}
	return nil
}

// GetAssertionAssignmentRuleByURN reads a rule from the OpenAPI v3 entity
// endpoint (MySQL, strongly consistent). Returns nil (no error) on 404.
func (c *Client) GetAssertionAssignmentRuleByURN(ctx context.Context, urn string) (*AssertionAssignmentRuleInfo, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("assignment rule URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/assertionassignmentrule/%s", urn)
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
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub assignment rule API: %s", res.StatusCode, respBody)
	}

	var entity assertionAssignmentRuleEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing assignment rule entity response: %w", err)
	}
	if entity.Key == nil && entity.Info == nil {
		return nil, nil
	}

	out := &AssertionAssignmentRuleInfo{}
	if entity.Key != nil {
		out.ID = entity.Key.Value.ID
	}
	if entity.Info != nil {
		v := entity.Info.Value
		out.Name = v.Name
		out.Mode = v.Mode
		out.Query = v.EntityFilter.JSON
		for _, group := range v.EntityFilter.Filter.Or {
			ag := AndFilter{}
			for _, f := range group.And {
				ag.And = append(ag.And, FacetFilter{
					Field:     f.Field,
					Values:    f.Values,
					Condition: f.Condition,
					Negated:   f.Negated,
				})
			}
			out.OrFilters = append(out.OrFilters, ag)
		}
		out.Freshness = readCategoryConfig(v.FreshnessConfig)
		out.Volume = readCategoryConfig(v.VolumeConfig)
	}
	return out, nil
}

// readCategoryConfig maps the read JSON to the client config model, returning
// nil when the category is absent or disabled.
func readCategoryConfig(cfg *ruleCategoryConfigJSON) *AssertionAssignmentRuleCategoryConfig {
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	out := &AssertionAssignmentRuleCategoryConfig{}
	if cfg.PreferredEvaluationParameters != nil {
		out.SourceType = cfg.PreferredEvaluationParameters.SourceType
	}
	for _, a := range cfg.OnSuccess {
		out.OnSuccessActions = append(out.OnSuccessActions, a.Type)
	}
	for _, a := range cfg.OnFailure {
		out.OnFailureActions = append(out.OnFailureActions, a.Type)
	}
	return out
}

// DeleteAssertionAssignmentRule deletes a rule by URN via the
// deleteAssertionAssignmentRule mutation. A not-found result is treated as
// success (the entity is already gone).
func (c *Client) DeleteAssertionAssignmentRule(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("assignment rule URN is required")
	}

	const q = `
mutation deleteAssertionAssignmentRule($urn: String!) {
  deleteAssertionAssignmentRule(urn: $urn)
}`
	body := map[string]any{"query": q, "variables": map[string]any{"urn": urn}}
	var raw genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &raw); err != nil {
		return err
	}
	if len(raw.Errors) > 0 {
		msg := raw.Errors[0].Message
		if strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist") {
			return nil
		}
		if isAssertionAssignmentRuleCloudOnlyError(msg) {
			return ErrAssertionAssignmentRuleCloudOnly
		}
		return fmt.Errorf("DataHub API error: %s", msg)
	}
	return nil
}

// ListAssertionAssignmentRuleURNs returns the URNs of all assignment rules via
// the listAssertionAssignmentRules query. Returns
// ErrAssertionAssignmentRuleCloudOnly on OSS. Backed by search (eventually
// consistent) -- for enumeration/import, not authoritative reads.
func (c *Client) ListAssertionAssignmentRuleURNs(ctx context.Context) ([]string, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}

	const q = `
query listAssertionAssignmentRules($input: ListAssertionAssignmentRulesInput!) {
  listAssertionAssignmentRules(input: $input) {
    total
    rules { urn }
  }
}`

	const pageSize = 100
	var urns []string
	start := 0
	for {
		body := map[string]any{
			"query":     q,
			"variables": map[string]any{"input": map[string]any{"start": start, "count": pageSize}},
		}
		var raw struct {
			Data struct {
				ListAssertionAssignmentRules struct {
					Total int `json:"total"`
					Rules []struct {
						URN string `json:"urn"`
					} `json:"rules"`
				} `json:"listAssertionAssignmentRules"`
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
			if isAssertionAssignmentRuleCloudOnlyError(msg) {
				return nil, ErrAssertionAssignmentRuleCloudOnly
			}
			return nil, fmt.Errorf("DataHub API error: %s", msg)
		}
		page := raw.Data.ListAssertionAssignmentRules.Rules
		for _, r := range page {
			if r.URN != "" {
				urns = append(urns, r.URN)
			}
		}
		start += len(page)
		if start >= raw.Data.ListAssertionAssignmentRules.Total || len(page) == 0 {
			break
		}
	}
	return urns, nil
}
