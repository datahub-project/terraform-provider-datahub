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

const (
	dataTypeURNPrefix   = "urn:li:dataType:datahub."
	entityTypeURNPrefix = "urn:li:entityType:datahub."
)

// dataTypeURN expands a short data-type name (e.g. "number") to a full
// DataHub dataType URN. If already a full URN it is returned unchanged.
func dataTypeURN(short string) string {
	if strings.HasPrefix(short, "urn:li:") {
		return short
	}
	return dataTypeURNPrefix + short
}

// shortDataType strips the "urn:li:dataType:datahub." prefix. If the URN does
// not have that prefix it is returned unchanged.
func shortDataType(urn string) string {
	return strings.TrimPrefix(urn, dataTypeURNPrefix)
}

// entityTypeURN expands a short entity-type name (e.g. "dataset") to a full
// DataHub entityType URN. If already a full URN it is returned unchanged.
func entityTypeURN(short string) string {
	if strings.HasPrefix(short, "urn:li:") {
		return short
	}
	return entityTypeURNPrefix + short
}

// shortEntityType strips the "urn:li:entityType:datahub." prefix. If the URN
// does not have that prefix it is returned unchanged.
func shortEntityType(urn string) string {
	return strings.TrimPrefix(urn, entityTypeURNPrefix)
}

// AllowedValue represents a single allowed value for a structured property.
// Exactly one of StringValue or NumberValue must be non-nil.
type AllowedValue struct {
	StringValue *string
	NumberValue *float64
	Description string
}

// StructuredPropertySettings mirrors the structuredPropertySettings aspect.
type StructuredPropertySettings struct {
	IsHidden                    bool
	ShowInSearchFilters         bool
	ShowInAssetSummary          bool
	HideInAssetSummaryWhenEmpty bool
	ShowAsAssetBadge            bool
	ShowInColumnsTable          bool
}

// StructuredProperty is the read-shape returned by GetStructuredPropertyByURN.
type StructuredProperty struct {
	URN                string
	ID                 string
	QualifiedName      string
	DisplayName        string
	Description        string
	ValueType          string // short name, e.g. "number"
	Cardinality        string // "SINGLE" | "MULTIPLE"
	Immutable          bool
	EntityTypes        []string // short names, e.g. ["dataset","dashboard"]
	AllowedValues      []AllowedValue
	AllowedEntityTypes []string // short names; from typeQualifier.allowedTypes
	Settings           *StructuredPropertySettings
}

// CreateStructuredPropertyInput groups the inputs for creating a structured
// property. Field names use short forms (e.g. ValueType = "number").
type CreateStructuredPropertyInput struct {
	// ID becomes the URN suffix and the qualifiedName. Required. Always supply
	// an explicit value; omitting it causes the server to generate a random UUID.
	ID                 string
	DisplayName        string
	Description        string
	ValueType          string // short name, e.g. "number"
	Cardinality        string // "SINGLE" | "MULTIPLE"; empty defaults to SINGLE
	Immutable          bool
	EntityTypes        []string // short names
	AllowedValues      []AllowedValue
	AllowedEntityTypes []string // short names; typeQualifier.allowedTypes
	Settings           *StructuredPropertySettings
}

// structuredPropertyEntity is the OpenAPI v3 response shape for
// GET /openapi/v3/entity/structuredproperty/{urn}.
type structuredPropertyEntity struct {
	URN     string `json:"urn"`
	KeyData *struct {
		Value struct {
			ID string `json:"id"`
		} `json:"value"`
	} `json:"structuredPropertyKey,omitempty"`
	PropDef *struct {
		Value struct {
			QualifiedName string   `json:"qualifiedName"`
			DisplayName   string   `json:"displayName"`
			Description   string   `json:"description"`
			ValueType     string   `json:"valueType"`
			Cardinality   string   `json:"cardinality"`
			Immutable     bool     `json:"immutable"`
			EntityTypes   []string `json:"entityTypes"`
			AllowedValues []struct {
				Value struct {
					String *string  `json:"string,omitempty"`
					Number *float64 `json:"double,omitempty"`
				} `json:"value"`
				Description string `json:"description"`
			} `json:"allowedValues"`
			TypeQualifier struct {
				AllowedTypes []string `json:"allowedTypes"`
			} `json:"typeQualifier"`
		} `json:"value"`
	} `json:"propertyDefinition,omitempty"`
	SettingsAspect *struct {
		Value struct {
			IsHidden                    bool `json:"isHidden"`
			ShowInSearchFilters         bool `json:"showInSearchFilters"`
			ShowInAssetSummary          bool `json:"showInAssetSummary"`
			HideInAssetSummaryWhenEmpty bool `json:"hideInAssetSummaryWhenEmpty"`
			ShowAsAssetBadge            bool `json:"showAsAssetBadge"`
			ShowInColumnsTable          bool `json:"showInColumnsTable"`
		} `json:"value"`
	} `json:"structuredPropertySettings,omitempty"`
}

// structuredPropertyURNPrefix is the URN namespace for structured properties.
const structuredPropertyURNPrefix = "urn:li:structuredProperty:"

// allowedValuesToAspect converts allowed values to the OpenAPI aspect shape:
// [{ "value": { "string": ... } | { "double": ... }, "description": ... }].
func allowedValuesToAspect(avs []AllowedValue) []map[string]any {
	out := make([]map[string]any, 0, len(avs))
	for _, av := range avs {
		primitive := map[string]any{}
		if av.StringValue != nil {
			primitive["string"] = *av.StringValue
		}
		if av.NumberValue != nil {
			primitive["double"] = *av.NumberValue
		}
		entry := map[string]any{"value": primitive}
		if av.Description != "" {
			entry["description"] = av.Description
		}
		out = append(out, entry)
	}
	return out
}

// structuredPropertyEntityPayload builds the OpenAPI v3 entity write body for a
// structured property: the propertyDefinition aspect, plus the
// structuredPropertySettings aspect when settings are supplied.
//
// The definition is written as a full aspect via OpenAPI rather than through
// the GraphQL createStructuredProperty/updateStructuredProperty mutations.
// Those mutations build a JSON Patch whose pointer path embeds each allowed
// value unescaped (StructuredPropertyDefinitionPatchBuilder.addAllowedValue),
// so any allowed string value containing "/" (or "~") is mis-parsed as nested
// pointer segments and the write fails with `/allowedValues/N/value :: field
// is required`. The OpenAPI entity write has no patch step and stores such
// values correctly. See Linear CAT-2551.
func structuredPropertyEntityPayload(urn string, in CreateStructuredPropertyInput) []map[string]any {
	entityTypeURNs := make([]string, len(in.EntityTypes))
	for i, et := range in.EntityTypes {
		entityTypeURNs[i] = entityTypeURN(et)
	}

	cardinality := in.Cardinality
	if cardinality == "" {
		cardinality = "SINGLE"
	}

	def := map[string]any{
		"qualifiedName": in.ID,
		"valueType":     dataTypeURN(in.ValueType),
		"entityTypes":   entityTypeURNs,
		"cardinality":   cardinality,
		"immutable":     in.Immutable,
	}
	if in.DisplayName != "" {
		def["displayName"] = in.DisplayName
	}
	if in.Description != "" {
		def["description"] = in.Description
	}
	if len(in.AllowedValues) > 0 {
		def["allowedValues"] = allowedValuesToAspect(in.AllowedValues)
	}
	if len(in.AllowedEntityTypes) > 0 {
		allowedTypeURNs := make([]string, len(in.AllowedEntityTypes))
		for i, t := range in.AllowedEntityTypes {
			allowedTypeURNs[i] = entityTypeURN(t)
		}
		def["typeQualifier"] = map[string]any{"allowedTypes": allowedTypeURNs}
	}

	entity := map[string]any{
		"urn":                urn,
		"propertyDefinition": map[string]any{"value": def},
	}

	if in.Settings != nil {
		s := in.Settings
		// Dependent field: when the property is not shown in the asset summary,
		// hideInAssetSummaryWhenEmpty must be false (mirrors the server resolver).
		hideWhenEmpty := s.HideInAssetSummaryWhenEmpty
		if !s.ShowInAssetSummary {
			hideWhenEmpty = false
		}
		entity["structuredPropertySettings"] = map[string]any{
			"value": map[string]any{
				"isHidden":                    s.IsHidden,
				"showInSearchFilters":         s.ShowInSearchFilters,
				"showInAssetSummary":          s.ShowInAssetSummary,
				"hideInAssetSummaryWhenEmpty": hideWhenEmpty,
				"showAsAssetBadge":            s.ShowAsAssetBadge,
				"showInColumnsTable":          s.ShowInColumnsTable,
			},
		}
	}

	return []map[string]any{entity}
}

// writeStructuredProperty writes the definition (and settings) aspect(s) via
// the OpenAPI v3 entity endpoint. Used by both create and update: the write is
// a full-aspect upsert, and the resource's plan modifiers force replacement
// (not update) on any list shrink or cardinality narrowing, so sending the full
// desired state on update is always safe.
func (c *Client) writeStructuredProperty(ctx context.Context, urn string, in CreateStructuredPropertyInput) error {
	payload := structuredPropertyEntityPayload(urn, in)
	req, err := c.NewRequest(ctx, http.MethodPost, "/openapi/v3/entity/structuredproperty?async=false", payload)
	if err != nil {
		return fmt.Errorf("building structured property write request: %w", err)
	}
	res, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("structured property write request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_STRUCTURED_PROPERTIES privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("unexpected HTTP %d from DataHub structured property write API: %s", res.StatusCode, respBody)
	}
	return nil
}

// CreateStructuredProperty creates a DataHub structured property via the
// OpenAPI v3 entity endpoint and returns its URN. Always supply a non-empty ID
// to produce a deterministic URN.
func (c *Client) CreateStructuredProperty(ctx context.Context, in CreateStructuredPropertyInput) (string, error) {
	if c == nil {
		return "", errors.New("client is nil")
	}
	if in.ID == "" {
		return "", errors.New("id is required")
	}
	if in.ValueType == "" {
		return "", errors.New("value_type is required")
	}
	if len(in.EntityTypes) == 0 {
		return "", errors.New("entity_types is required and must not be empty")
	}

	urn := structuredPropertyURNPrefix + in.ID

	// The OpenAPI write is an upsert, so guard against silently overwriting a
	// property that already exists out-of-band (the GraphQL create used to
	// reject this server-side).
	existing, err := c.GetStructuredPropertyByURN(ctx, urn)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return "", fmt.Errorf("a structured property already exists with URN %s", urn)
	}

	if err := c.writeStructuredProperty(ctx, urn, in); err != nil {
		return "", err
	}
	return urn, nil
}

// GetStructuredPropertyByURN fetches a structured property directly by URN via
// the OpenAPI v3 entity endpoint (MySQL, strongly consistent).
// Returns nil (no error) on 404.
func (c *Client) GetStructuredPropertyByURN(ctx context.Context, urn string) (*StructuredProperty, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return nil, errors.New("URN is required")
	}

	path := fmt.Sprintf("/openapi/v3/entity/structuredproperty/%s", urn)
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
		return nil, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the MANAGE_STRUCTURED_PROPERTIES privilege", res.StatusCode)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d from DataHub structured property API: %s", res.StatusCode, respBody)
	}

	var entity structuredPropertyEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing structured property entity response: %w", err)
	}

	if entity.KeyData == nil && entity.PropDef == nil {
		return nil, nil
	}

	id := ""
	if entity.KeyData != nil {
		id = entity.KeyData.Value.ID
	}
	if id == "" {
		id = strings.TrimPrefix(entity.URN, "urn:li:structuredProperty:")
	}

	sp := &StructuredProperty{URN: entity.URN, ID: id, QualifiedName: id}

	if entity.PropDef != nil {
		v := entity.PropDef.Value
		sp.QualifiedName = v.QualifiedName
		if sp.ID == "" {
			sp.ID = v.QualifiedName
		}
		sp.DisplayName = v.DisplayName
		sp.Description = v.Description
		sp.ValueType = shortDataType(v.ValueType)
		sp.Cardinality = v.Cardinality
		if sp.Cardinality == "" {
			sp.Cardinality = "SINGLE"
		}
		sp.Immutable = v.Immutable

		sp.EntityTypes = make([]string, len(v.EntityTypes))
		for i, et := range v.EntityTypes {
			sp.EntityTypes[i] = shortEntityType(et)
		}

		if len(v.AllowedValues) > 0 {
			sp.AllowedValues = make([]AllowedValue, len(v.AllowedValues))
			for i, av := range v.AllowedValues {
				sp.AllowedValues[i] = AllowedValue{
					StringValue: av.Value.String,
					NumberValue: av.Value.Number,
					Description: av.Description,
				}
			}
		}

		if len(v.TypeQualifier.AllowedTypes) > 0 {
			sp.AllowedEntityTypes = make([]string, len(v.TypeQualifier.AllowedTypes))
			for i, at := range v.TypeQualifier.AllowedTypes {
				sp.AllowedEntityTypes[i] = shortEntityType(at)
			}
		}
	}

	if entity.SettingsAspect != nil {
		s := entity.SettingsAspect.Value
		sp.Settings = &StructuredPropertySettings{
			IsHidden:                    s.IsHidden,
			ShowInSearchFilters:         s.ShowInSearchFilters,
			ShowInAssetSummary:          s.ShowInAssetSummary,
			HideInAssetSummaryWhenEmpty: s.HideInAssetSummaryWhenEmpty,
			ShowAsAssetBadge:            s.ShowAsAssetBadge,
			ShowInColumnsTable:          s.ShowInColumnsTable,
		}
	}

	return sp, nil
}

// UpdateStructuredProperty updates an existing structured property by writing
// the full definition (and settings) aspect via the OpenAPI v3 entity endpoint.
// The resource forces replacement (not update) on any list shrink or
// cardinality narrowing, so the full desired state passed here is always a
// superset or scalar change - safe to write wholesale.
func (c *Client) UpdateStructuredProperty(ctx context.Context, urn string, in CreateStructuredPropertyInput) error {
	if c == nil {
		return errors.New("client is nil")
	}
	if urn == "" {
		return errors.New("URN is required")
	}
	if in.ValueType == "" {
		return errors.New("value_type is required")
	}
	if len(in.EntityTypes) == 0 {
		return errors.New("entity_types is required and must not be empty")
	}
	return c.writeStructuredProperty(ctx, urn, in)
}

// DeleteStructuredProperty hard-deletes a DataHub structured property by URN
// via the deleteStructuredProperty GraphQL mutation. Deletion also asynchronously
// removes applied values from all assets. Structured properties are flat (no
// children), so no child-guard or retry logic is needed.
func (c *Client) DeleteStructuredProperty(ctx context.Context, urn string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return errors.New("URN is required")
	}

	const q = `
mutation deleteStructuredProperty($input: DeleteStructuredPropertyInput!) {
  deleteStructuredProperty(input: $input)
}`

	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{"urn": urn},
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
