// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"net/http"
)

// spMockValue is one assigned structured-property value (string or number).
type spMockValue struct {
	String *string
	Double *float64
}

// handleUpsertStructuredProperties handles the upsertStructuredProperties GraphQL
// mutation with per-property MERGE semantics: each named property's value list is
// replaced, while any other properties already on the entity are left intact.
func (s *mockServer) handleUpsertStructuredProperties(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	assetURN, _ := input["assetUrn"].(string)
	params, _ := input["structuredPropertyInputParams"].([]any)

	s.mu.Lock()
	if s.entityStructuredProps[assetURN] == nil {
		s.entityStructuredProps[assetURN] = make(map[string][]spMockValue)
	}
	for _, p := range params {
		param, _ := p.(map[string]any)
		propURN, _ := param["structuredPropertyUrn"].(string)
		rawValues, _ := param["values"].([]any)
		vals := make([]spMockValue, 0, len(rawValues))
		for _, rv := range rawValues {
			v, _ := rv.(map[string]any)
			mv := spMockValue{}
			if sv, ok := v["stringValue"].(string); ok {
				mv.String = &sv
			}
			if nv, ok := v["numberValue"].(float64); ok {
				mv.Double = &nv
			}
			vals = append(vals, mv)
		}
		s.entityStructuredProps[assetURN][propURN] = vals
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"upsertStructuredProperties": map[string]any{"properties": []any{}},
		},
	})
}

// handleRemoveStructuredProperties handles the removeStructuredProperties GraphQL
// mutation: removes the named properties from the entity, leaving others intact.
func (s *mockServer) handleRemoveStructuredProperties(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	assetURN, _ := input["assetUrn"].(string)
	urns, _ := input["structuredPropertyUrns"].([]any)

	s.mu.Lock()
	if m := s.entityStructuredProps[assetURN]; m != nil {
		for _, u := range urns {
			if propURN, ok := u.(string); ok {
				delete(m, propURN)
			}
		}
		if len(m) == 0 {
			delete(s.entityStructuredProps, assetURN)
		}
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"removeStructuredProperties": map[string]any{"properties": []any{}},
		},
	})
}

// structuredPropertiesAspect returns the OpenAPI v3 structuredProperties aspect
// for a target entity, or nil when the entity has no assignments. Takes its own
// lock; call it from item handlers AFTER they have released s.mu.
func (s *mockServer) structuredPropertiesAspect(entityURN string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	assignments := s.entityStructuredProps[entityURN]
	if len(assignments) == 0 {
		return nil
	}
	properties := make([]map[string]any, 0, len(assignments))
	for propURN, vals := range assignments {
		values := make([]map[string]any, 0, len(vals))
		for _, v := range vals {
			valueMap := map[string]any{}
			if v.String != nil {
				valueMap["string"] = *v.String
			}
			if v.Double != nil {
				valueMap["double"] = *v.Double
			}
			values = append(values, valueMap)
		}
		properties = append(properties, map[string]any{
			"propertyUrn": propURN,
			"values":      values,
		})
	}
	return map[string]any{"value": map[string]any{"properties": properties}}
}
