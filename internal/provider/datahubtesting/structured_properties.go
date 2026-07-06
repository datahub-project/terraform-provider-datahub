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

// handleStructuredPropertyWrite serves POST /openapi/v3/entity/structuredproperty,
// the full-aspect definition (+settings) upsert the provider performs for both
// create and update. It fully replaces the stored entity from the payload,
// mirroring the real OpenAPI v3 semantics - so if the provider ever dropped a
// field from this write, the read-back would reflect the loss.
func (s *mockServer) handleStructuredPropertyWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var entities []map[string]any
	if err := json.NewDecoder(r.Body).Decode(&entities); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	for _, e := range entities {
		urn, _ := e["urn"].(string)
		id := strings.TrimPrefix(urn, "urn:li:structuredProperty:")
		def := aspectValueMap(e["propertyDefinition"])

		sp := mockStructuredProperty{URN: urn, ID: id, Cardinality: "SINGLE"}
		if v, ok := def["qualifiedName"].(string); ok && v != "" {
			sp.ID = v
		}
		if v, ok := def["displayName"].(string); ok {
			sp.DisplayName = v
		}
		if v, ok := def["description"].(string); ok {
			sp.Description = v
		}
		if v, ok := def["valueType"].(string); ok {
			sp.ValueType = v
		}
		if v, ok := def["cardinality"].(string); ok && v != "" {
			sp.Cardinality = v
		}
		if v, ok := def["immutable"].(bool); ok {
			sp.Immutable = v
		}
		sp.EntityTypes = anySliceToStrings(def["entityTypes"])

		if avRaw, ok := def["allowedValues"].([]any); ok {
			for _, avAny := range avRaw {
				av, _ := avAny.(map[string]any)
				mav := mockAllowedValue{}
				if valMap, ok := av["value"].(map[string]any); ok {
					if sv, ok := valMap["string"].(string); ok {
						mav.StringValue = &sv
					}
					if nv, ok := valMap["double"].(float64); ok {
						mav.NumberValue = &nv
					}
				}
				if d, ok := av["description"].(string); ok {
					mav.Description = d
				}
				sp.AllowedValues = append(sp.AllowedValues, mav)
			}
		}

		if tqRaw, ok := def["typeQualifier"].(map[string]any); ok {
			sp.AllowedEntityTypes = anySliceToStrings(tqRaw["allowedTypes"])
		}

		if settVal := aspectValueMap(e["structuredPropertySettings"]); settVal != nil {
			sp.Settings = extractMockSPSettings(settVal)
			sp.SettingsSet = true
		}

		s.structuredProperties[sp.ID] = sp
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entities)
}

// aspectValueMap returns the inner "value" object of an OpenAPI aspect wrapper
// ({"value": {...}}), or nil if the aspect is absent.
func aspectValueMap(aspect any) map[string]any {
	wrapper, ok := aspect.(map[string]any)
	if !ok {
		return nil
	}
	val, _ := wrapper["value"].(map[string]any)
	return val
}

// anySliceToStrings converts a decoded JSON array to a []string.
func anySliceToStrings(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
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
