// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

	type parsedParam struct {
		propURN string
		vals    []spMockValue
	}
	items := make([]parsedParam, 0, len(params))
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
		items = append(items, parsedParam{propURN: propURN, vals: vals})
	}

	s.mu.Lock()
	// Mirror the server's StructuredPropertiesValidator: reject values that
	// violate the property definition's cardinality or allowed values. This is
	// what makes the provider's error-surfacing path testable in-mock.
	for _, it := range items {
		id := strings.TrimPrefix(it.propURN, "urn:li:structuredProperty:")
		def, ok := s.structuredProperties[id]
		if !ok {
			continue
		}
		if def.Cardinality == "SINGLE" && len(it.vals) > 1 {
			s.mu.Unlock()
			writeSPMockError(w, fmt.Sprintf("Property: %s has cardinality 1, but multiple values were assigned: %d", it.propURN, len(it.vals)))
			return
		}
		if len(def.AllowedValues) > 0 {
			allowed := make(map[string]bool)
			for _, av := range def.AllowedValues {
				if av.StringValue != nil {
					allowed["s:"+*av.StringValue] = true
				}
				if av.NumberValue != nil {
					allowed[fmt.Sprintf("n:%v", *av.NumberValue)] = true
				}
			}
			for _, v := range it.vals {
				key, display := "", ""
				switch {
				case v.String != nil:
					key, display = "s:"+*v.String, *v.String
				case v.Double != nil:
					key, display = fmt.Sprintf("n:%v", *v.Double), fmt.Sprintf("%v", *v.Double)
				}
				if !allowed[key] {
					s.mu.Unlock()
					writeSPMockError(w, fmt.Sprintf("Property: %s, value: %s should be one of the allowed values", it.propURN, display))
					return
				}
			}
		}
	}

	if s.entityStructuredProps[assetURN] == nil {
		s.entityStructuredProps[assetURN] = make(map[string][]spMockValue)
	}
	for _, it := range items {
		s.entityStructuredProps[assetURN][it.propURN] = it.vals
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"upsertStructuredProperties": map[string]any{"properties": []any{}},
		},
	})
}

// writeSPMockError writes a GraphQL-style errors response (HTTP 200 with an
// errors array), the shape the client inspects for API errors.
func writeSPMockError(w http.ResponseWriter, msg string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]any{{"message": msg}},
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

// spExistsFilterField extracts the field name from a searchAcrossEntities
// input whose orFilters contain a single EXISTS criterion on a
// "structuredProperties." index field, or "" when the input is any other
// search. This is the query shape the provider's delete settle-barrier sends.
func spExistsFilterField(input map[string]any) string {
	orFilters, _ := input["orFilters"].([]any)
	for _, ofAny := range orFilters {
		of, _ := ofAny.(map[string]any)
		ands, _ := of["and"].([]any)
		for _, cAny := range ands {
			criterion, _ := cAny.(map[string]any)
			field, _ := criterion["field"].(string)
			condition, _ := criterion["condition"].(string)
			if condition == "EXISTS" && strings.HasPrefix(field, "structuredProperties.") {
				return field
			}
		}
	}
	return ""
}

// handleSearchEntitiesWithStructuredProperty answers the settle-barrier's
// EXISTS query: it counts stored assignments whose property URN maps to the
// requested index field (qualified name with dots replaced by underscores).
func (s *mockServer) handleSearchEntitiesWithStructuredProperty(w http.ResponseWriter, field string) {
	fieldName := strings.TrimPrefix(field, "structuredProperties.")

	s.mu.Lock()
	var results []map[string]any
	for entityURN, props := range s.entityStructuredProps {
		for propURN := range props {
			id := strings.TrimPrefix(propURN, "urn:li:structuredProperty:")
			if strings.ReplaceAll(id, ".", "_") == fieldName {
				results = append(results, map[string]any{
					"entity": map[string]any{"urn": entityURN},
				})
				break
			}
		}
	}
	s.mu.Unlock()

	if results == nil {
		results = []map[string]any{}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"searchAcrossEntities": map[string]any{
				"total":         len(results),
				"searchResults": results,
			},
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
