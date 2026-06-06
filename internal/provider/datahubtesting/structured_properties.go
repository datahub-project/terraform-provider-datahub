// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"net/http"
	"strings"
)

// mockStructuredProperty mirrors the structured property shape the provider
// sends and reads.
type mockStructuredProperty struct {
	URN                string
	ID                 string
	DisplayName        string
	Description        string
	ValueType          string // full URN, e.g. "urn:li:dataType:datahub.number"
	Cardinality        string // "SINGLE" | "MULTIPLE"
	Immutable          bool
	EntityTypes        []string // full URNs
	AllowedValues      []mockAllowedValue
	AllowedEntityTypes []string // full URNs; typeQualifier.allowedTypes
	Settings           mockSPSettings
	SettingsSet        bool // true when settings were explicitly configured
}

type mockAllowedValue struct {
	StringValue *string
	NumberValue *float64
	Description string
}

type mockSPSettings struct {
	IsHidden                    bool
	ShowInSearchFilters         bool
	ShowInAssetSummary          bool
	HideInAssetSummaryWhenEmpty bool
	ShowAsAssetBadge            bool
	ShowInColumnsTable          bool
}

// handleCreateStructuredProperty handles the createStructuredProperty GraphQL
// mutation.
func (s *mockServer) handleCreateStructuredProperty(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	id, _ := input["id"].(string)
	if id == "" {
		// Fallback to qualifiedName.
		id, _ = input["qualifiedName"].(string)
	}
	urn := "urn:li:structuredProperty:" + id

	sp := mockStructuredProperty{
		URN:         urn,
		ID:          id,
		Cardinality: "SINGLE",
	}

	if dn, ok := input["displayName"].(string); ok {
		sp.DisplayName = dn
	}
	if desc, ok := input["description"].(string); ok {
		sp.Description = desc
	}
	if vt, ok := input["valueType"].(string); ok {
		sp.ValueType = vt
	}
	if card, ok := input["cardinality"].(string); ok {
		sp.Cardinality = card
	}
	if imm, ok := input["immutable"].(bool); ok {
		sp.Immutable = imm
	}

	// Entity types.
	if etRaw, ok := input["entityTypes"].([]any); ok {
		for _, et := range etRaw {
			if s, ok := et.(string); ok {
				sp.EntityTypes = append(sp.EntityTypes, s)
			}
		}
	}

	// Allowed values.
	if avRaw, ok := input["allowedValues"].([]any); ok {
		for _, avAny := range avRaw {
			av, _ := avAny.(map[string]any)
			mav := mockAllowedValue{}
			if sv, ok := av["stringValue"].(string); ok {
				mav.StringValue = &sv
			}
			if nv, ok := av["numberValue"].(float64); ok {
				mav.NumberValue = &nv
			}
			if d, ok := av["description"].(string); ok {
				mav.Description = d
			}
			sp.AllowedValues = append(sp.AllowedValues, mav)
		}
	}

	// typeQualifier.allowedTypes.
	if tqRaw, ok := input["typeQualifier"].(map[string]any); ok {
		if atRaw, ok := tqRaw["allowedTypes"].([]any); ok {
			for _, at := range atRaw {
				if s, ok := at.(string); ok {
					sp.AllowedEntityTypes = append(sp.AllowedEntityTypes, s)
				}
			}
		}
	}

	// Settings.
	if settRaw, ok := input["settings"].(map[string]any); ok {
		sp.Settings = extractMockSPSettings(settRaw)
		sp.SettingsSet = true
	}

	s.mu.Lock()
	s.structuredProperties[id] = sp
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"createStructuredProperty": map[string]any{"urn": urn},
		},
	})
}

// handleUpdateStructuredProperty handles the updateStructuredProperty GraphQL
// mutation. Applies server append-only semantics: lists grow, scalar fields
// are replaced, cardinality only widens.
func (s *mockServer) handleUpdateStructuredProperty(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	urn, _ := input["urn"].(string)
	id := strings.TrimPrefix(urn, "urn:li:structuredProperty:")

	s.mu.Lock()
	sp, ok := s.structuredProperties[id]
	if !ok {
		s.mu.Unlock()
		http.Error(w, `{"errors":[{"message":"structured property not found"}]}`, http.StatusNotFound)
		return
	}

	// Scalar replacements.
	if dn, ok := input["displayName"].(string); ok {
		sp.DisplayName = dn
	}
	if desc, ok := input["description"].(string); ok {
		sp.Description = desc
	}
	if imm, ok := input["immutable"].(bool); ok {
		sp.Immutable = imm
	}
	// Cardinality: only widen.
	if wide, ok := input["setCardinalityAsMultiple"].(bool); ok && wide {
		sp.Cardinality = "MULTIPLE"
	}

	// Append-only: new entity types.
	if etRaw, ok := input["newEntityTypes"].([]any); ok {
		existingSet := make(map[string]bool)
		for _, et := range sp.EntityTypes {
			existingSet[et] = true
		}
		for _, et := range etRaw {
			if s, ok := et.(string); ok && !existingSet[s] {
				sp.EntityTypes = append(sp.EntityTypes, s)
			}
		}
	}

	// Append-only: new allowed values.
	if avRaw, ok := input["newAllowedValues"].([]any); ok {
		for _, avAny := range avRaw {
			av, _ := avAny.(map[string]any)
			mav := mockAllowedValue{}
			if sv, ok := av["stringValue"].(string); ok {
				mav.StringValue = &sv
			}
			if nv, ok := av["numberValue"].(float64); ok {
				mav.NumberValue = &nv
			}
			if d, ok := av["description"].(string); ok {
				mav.Description = d
			}
			sp.AllowedValues = append(sp.AllowedValues, mav)
		}
	}

	// Append-only: new allowed entity types.
	if tqRaw, ok := input["typeQualifier"].(map[string]any); ok {
		if atRaw, ok := tqRaw["newAllowedTypes"].([]any); ok {
			existingSet := make(map[string]bool)
			for _, at := range sp.AllowedEntityTypes {
				existingSet[at] = true
			}
			for _, at := range atRaw {
				if s, ok := at.(string); ok && !existingSet[s] {
					sp.AllowedEntityTypes = append(sp.AllowedEntityTypes, s)
				}
			}
		}
	}

	// Settings: full replace.
	if settRaw, ok := input["settings"].(map[string]any); ok {
		sp.Settings = extractMockSPSettings(settRaw)
		sp.SettingsSet = true
	}

	s.structuredProperties[id] = sp
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"updateStructuredProperty": map[string]any{"urn": urn},
		},
	})
}

// handleDeleteStructuredProperty handles the deleteStructuredProperty GraphQL
// mutation.
func (s *mockServer) handleDeleteStructuredProperty(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	urn, _ := input["urn"].(string)
	id := strings.TrimPrefix(urn, "urn:li:structuredProperty:")

	s.mu.Lock()
	delete(s.structuredProperties, id)
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"deleteStructuredProperty": true},
	})
}

// handleStructuredPropertyItem serves
// GET /openapi/v3/entity/structuredproperty/{urn}, returning the
// propertyDefinition + structuredPropertySettings aspect shape.
func (s *mockServer) handleStructuredPropertyItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/structuredproperty/")
	id := strings.TrimPrefix(urn, "urn:li:structuredProperty:")

	s.mu.Lock()
	sp, ok := s.structuredProperties[id]
	s.mu.Unlock()

	if !ok {
		http.NotFound(w, r)
		return
	}

	// Build allowedValues in the PDL union-encoded shape the client expects.
	// The PDL union [string, double] serialises as {"string": ...} / {"double": ...}.
	avList := make([]map[string]any, len(sp.AllowedValues))
	for i, av := range sp.AllowedValues {
		valueMap := map[string]any{}
		if av.StringValue != nil {
			valueMap["string"] = *av.StringValue
		}
		if av.NumberValue != nil {
			valueMap["double"] = *av.NumberValue
		}
		avList[i] = map[string]any{
			"value":       valueMap,
			"description": av.Description,
		}
	}

	propDef := map[string]any{
		"value": map[string]any{
			"qualifiedName": id,
			"displayName":   sp.DisplayName,
			"description":   sp.Description,
			"valueType":     sp.ValueType,
			"cardinality":   sp.Cardinality,
			"immutable":     sp.Immutable,
			"entityTypes":   sp.EntityTypes,
			"allowedValues": avList,
			"typeQualifier": map[string]any{
				"allowedTypes": sp.AllowedEntityTypes,
			},
		},
	}

	entity := map[string]any{
		"urn": sp.URN,
		"structuredPropertyKey": map[string]any{
			"value": map[string]any{"id": sp.ID},
		},
		"propertyDefinition": propDef,
	}

	// Only include the structuredPropertySettings aspect when settings were
	// explicitly configured (mirrors real server behavior: the aspect is absent
	// if settings were never written).
	if sp.SettingsSet {
		entity["structuredPropertySettings"] = map[string]any{
			"value": map[string]any{
				"isHidden":                    sp.Settings.IsHidden,
				"showInSearchFilters":         sp.Settings.ShowInSearchFilters,
				"showInAssetSummary":          sp.Settings.ShowInAssetSummary,
				"hideInAssetSummaryWhenEmpty": sp.Settings.HideInAssetSummaryWhenEmpty,
				"showAsAssetBadge":            sp.Settings.ShowAsAssetBadge,
				"showInColumnsTable":          sp.Settings.ShowInColumnsTable,
			},
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entity)
}

// extractMockSPSettings parses a settings map from a GraphQL input.
func extractMockSPSettings(m map[string]any) mockSPSettings {
	s := mockSPSettings{}
	if v, ok := m["isHidden"].(bool); ok {
		s.IsHidden = v
	}
	if v, ok := m["showInSearchFilters"].(bool); ok {
		s.ShowInSearchFilters = v
	}
	if v, ok := m["showInAssetSummary"].(bool); ok {
		s.ShowInAssetSummary = v
	}
	if v, ok := m["hideInAssetSummaryWhenEmpty"].(bool); ok {
		s.HideInAssetSummaryWhenEmpty = v
	}
	if v, ok := m["showAsAssetBadge"].(bool); ok {
		s.ShowAsAssetBadge = v
	}
	if v, ok := m["showInColumnsTable"].(bool); ok {
		s.ShowInColumnsTable = v
	}
	return s
}
