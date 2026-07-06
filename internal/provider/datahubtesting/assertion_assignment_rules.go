// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"net/http"
	"strings"
)

const mockAssignmentRuleURNPrefix = "urn:li:assertionAssignmentRule:"

// mockAssignmentRule stores the in-memory state for one assignment rule.
type mockAssignmentRule struct {
	ID        string
	Name      string
	Mode      string
	Query     string
	OrFilters []any          // raw orFilters input: [{and:[{field,values,condition,negated}]}]
	Freshness map[string]any // raw freshnessConfig input; nil when unset
	Volume    map[string]any // raw volumeConfig input; nil when unset
}

// ruleFromInput populates a rule from a create/update input map.
func ruleFromInput(id string, input map[string]any) mockAssignmentRule {
	rule := mockAssignmentRule{ID: id, Mode: "ENABLED"}
	rule.Name, _ = input["name"].(string)
	rule.Query, _ = input["filter"].(string)
	if of, ok := input["orFilters"].([]any); ok {
		rule.OrFilters = of
	}
	if fc, ok := input["freshnessConfig"].(map[string]any); ok {
		rule.Freshness = fc
	}
	if vc, ok := input["volumeConfig"].(map[string]any); ok {
		rule.Volume = vc
	}
	if m, ok := input["mode"].(string); ok && m != "" {
		rule.Mode = m
	}
	return rule
}

func (s *mockServer) handleCreateAssignmentRule(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	id, _ := input["id"].(string)

	rule := ruleFromInput(id, input)

	s.mu.Lock()
	s.assignmentRules[id] = rule
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"createAssertionAssignmentRule": map[string]any{"urn": mockAssignmentRuleURNPrefix + id},
		},
	})
}

func (s *mockServer) handleUpdateAssignmentRule(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	id := strings.TrimPrefix(urn, mockAssignmentRuleURNPrefix)
	input, _ := variables["input"].(map[string]any)

	s.mu.Lock()
	existing, ok := s.assignmentRules[id]
	rule := ruleFromInput(id, input)
	// Preserve the prior mode when the update omits it.
	if _, sent := input["mode"]; !sent && ok {
		rule.Mode = existing.Mode
	}
	s.assignmentRules[id] = rule
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"updateAssertionAssignmentRule": map[string]any{"urn": urn},
		},
	})
}

func (s *mockServer) handleDeleteAssignmentRule(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	id := strings.TrimPrefix(urn, mockAssignmentRuleURNPrefix)

	s.mu.Lock()
	delete(s.assignmentRules, id)
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"deleteAssertionAssignmentRule": true},
	})
}

func (s *mockServer) handleListAssignmentRules(w http.ResponseWriter) {
	s.mu.Lock()
	rules := make([]map[string]any, 0, len(s.assignmentRules))
	for id := range s.assignmentRules {
		rules = append(rules, map[string]any{"urn": mockAssignmentRuleURNPrefix + id})
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"listAssertionAssignmentRules": map[string]any{
				"total": len(rules),
				"rules": rules,
			},
		},
	})
}

// readCategoryConfig transforms a stored config input into the read shape,
// moving sourceType under preferredEvaluationParameters and marking it enabled.
func readCategoryConfig(cfg map[string]any) map[string]any {
	if cfg == nil {
		return nil
	}
	out := map[string]any{"enabled": true}
	if st, ok := cfg["sourceType"].(string); ok && st != "" {
		out["preferredEvaluationParameters"] = map[string]any{"sourceType": st}
	}
	if os, ok := cfg["onSuccess"]; ok {
		out["onSuccess"] = os
	}
	if of, ok := cfg["onFailure"]; ok {
		out["onFailure"] = of
	}
	return out
}

// handleAssignmentRuleItem handles GET /openapi/v3/entity/assertionassignmentrule/{urn}.
func (s *mockServer) handleAssignmentRuleItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/assertionassignmentrule/")
	id := strings.TrimPrefix(urn, mockAssignmentRuleURNPrefix)

	s.mu.Lock()
	rule, ok := s.assignmentRules[id]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	infoValue := map[string]any{
		"mode": rule.Mode,
		"name": rule.Name,
		"entityFilter": map[string]any{
			"json":   rule.Query,
			"filter": map[string]any{"or": rule.OrFilters},
		},
	}
	if fc := readCategoryConfig(rule.Freshness); fc != nil {
		infoValue["freshnessConfig"] = fc
	}
	if vc := readCategoryConfig(rule.Volume); vc != nil {
		infoValue["volumeConfig"] = vc
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"urn":                         mockAssignmentRuleURNPrefix + id,
		"assertionAssignmentRuleKey":  map[string]any{"value": map[string]any{"id": id}},
		"assertionAssignmentRuleInfo": map[string]any{"value": infoValue},
	})
}
