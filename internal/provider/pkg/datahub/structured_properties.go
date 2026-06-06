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

// UpdateStructuredPropertyInput groups the fields for an in-place update.
// Only additive/scalar changes should be sent here; shrink operations are
// handled by resource replacement at plan time.
type UpdateStructuredPropertyInput struct {
	URN                    string
	DisplayName            string
	Description            string
	Immutable              *bool
	NewEntityTypes         []string       // delta: elements in plan but not in state
	NewAllowedValues       []AllowedValue // delta: elements in plan but not in state
	NewAllowedEntityTypes  []string       // delta: elements in plan but not in state
	SetCardinalityMultiple bool           // true when widening SINGLE -> MULTIPLE
	Settings               *StructuredPropertySettings
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

type createStructuredPropertyResponse struct {
	Data struct {
		CreateStructuredProperty struct {
			URN string `json:"urn"`
		} `json:"createStructuredProperty"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// allowedValueToGQL converts an AllowedValue to the GraphQL input map.
func allowedValueToGQL(av AllowedValue) map[string]any {
	m := map[string]any{}
	if av.StringValue != nil {
		m["stringValue"] = *av.StringValue
	}
	if av.NumberValue != nil {
		m["numberValue"] = *av.NumberValue
	}
	if av.Description != "" {
		m["description"] = av.Description
	}
	return m
}

// settingsToGQL converts StructuredPropertySettings to the GraphQL input map.
func settingsToGQL(s *StructuredPropertySettings) map[string]any {
	if s == nil {
		return nil
	}
	return map[string]any{
		"isHidden":                    s.IsHidden,
		"showInSearchFilters":         s.ShowInSearchFilters,
		"showInAssetSummary":          s.ShowInAssetSummary,
		"hideInAssetSummaryWhenEmpty": s.HideInAssetSummaryWhenEmpty,
		"showAsAssetBadge":            s.ShowAsAssetBadge,
		"showInColumnsTable":          s.ShowInColumnsTable,
	}
}

// CreateStructuredProperty creates a DataHub structured property via the
// GraphQL API and returns its URN. Always supply a non-empty ID to produce a
// deterministic URN; omitting it causes the server to generate a random UUID.
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

	const q = `
mutation createStructuredProperty($input: CreateStructuredPropertyInput!) {
  createStructuredProperty(input: $input) {
    urn
  }
}`

	// Build entity-type URNs and allowed-type URNs.
	entityTypeURNs := make([]string, len(in.EntityTypes))
	for i, et := range in.EntityTypes {
		entityTypeURNs[i] = entityTypeURN(et)
	}

	input := map[string]any{
		"id":            in.ID,
		"qualifiedName": in.ID,
		"valueType":     dataTypeURN(in.ValueType),
		"entityTypes":   entityTypeURNs,
	}
	if in.DisplayName != "" {
		input["displayName"] = in.DisplayName
	}
	if in.Description != "" {
		input["description"] = in.Description
	}
	if in.Immutable {
		input["immutable"] = true
	}
	cardinality := in.Cardinality
	if cardinality == "" {
		cardinality = "SINGLE"
	}
	input["cardinality"] = cardinality

	if len(in.AllowedValues) > 0 {
		avs := make([]map[string]any, len(in.AllowedValues))
		for i, av := range in.AllowedValues {
			avs[i] = allowedValueToGQL(av)
		}
		input["allowedValues"] = avs
	}

	if len(in.AllowedEntityTypes) > 0 {
		allowedTypeURNs := make([]string, len(in.AllowedEntityTypes))
		for i, t := range in.AllowedEntityTypes {
			allowedTypeURNs[i] = entityTypeURN(t)
		}
		input["typeQualifier"] = map[string]any{
			"allowedTypes": allowedTypeURNs,
		}
	}

	if in.Settings != nil {
		input["settings"] = settingsToGQL(in.Settings)
	}

	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"input": input},
	}

	var gqlResp createStructuredPropertyResponse
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return "", err
	}
	if len(gqlResp.Errors) > 0 {
		return "", fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}

	urn := gqlResp.Data.CreateStructuredProperty.URN
	if urn == "" {
		urn = "urn:li:structuredProperty:" + in.ID
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

// UpdateStructuredProperty updates an existing structured property via the
// GraphQL updateStructuredProperty mutation. The update is additive for list
// fields; shrink operations must be handled by resource replacement.
func (c *Client) UpdateStructuredProperty(ctx context.Context, in UpdateStructuredPropertyInput) error {
	if c == nil {
		return errors.New("client is nil")
	}
	if in.URN == "" {
		return errors.New("URN is required")
	}

	const q = `
mutation updateStructuredProperty($input: UpdateStructuredPropertyInput!) {
  updateStructuredProperty(input: $input) {
    urn
  }
}`

	input := map[string]any{
		"urn": in.URN,
	}
	if in.DisplayName != "" {
		input["displayName"] = in.DisplayName
	}
	if in.Description != "" {
		input["description"] = in.Description
	}
	if in.Immutable != nil {
		input["immutable"] = *in.Immutable
	}
	if in.SetCardinalityMultiple {
		input["setCardinalityAsMultiple"] = true
	}

	if len(in.NewEntityTypes) > 0 {
		entityTypeURNs := make([]string, len(in.NewEntityTypes))
		for i, et := range in.NewEntityTypes {
			entityTypeURNs[i] = entityTypeURN(et)
		}
		input["newEntityTypes"] = entityTypeURNs
	}

	if len(in.NewAllowedValues) > 0 {
		avs := make([]map[string]any, len(in.NewAllowedValues))
		for i, av := range in.NewAllowedValues {
			avs[i] = allowedValueToGQL(av)
		}
		input["newAllowedValues"] = avs
	}

	if len(in.NewAllowedEntityTypes) > 0 {
		allowedTypeURNs := make([]string, len(in.NewAllowedEntityTypes))
		for i, t := range in.NewAllowedEntityTypes {
			allowedTypeURNs[i] = entityTypeURN(t)
		}
		input["typeQualifier"] = map[string]any{
			"newAllowedTypes": allowedTypeURNs,
		}
	}

	if in.Settings != nil {
		input["settings"] = settingsToGQL(in.Settings)
	}

	body := map[string]any{
		"query":     q,
		"variables": map[string]any{"input": input},
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
