// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"net/http"
	"strings"
)

// mockPolicy mirrors the dataHubPolicyInfo shape the provider sends and reads.
type mockPolicy struct {
	ID                  string
	Name                string
	Type                string
	State               string
	Description         string
	Privileges          []string
	Users               []string
	Groups              []string
	ResourceOwnersTypes []string
	AllUsers            bool
	AllGroups           bool
	ResourceOwners      bool
	HasResources        bool
	ResType             string
	ResResources        []string
	ResAll              bool
}

// handleUpsertPolicy handles the updatePolicy mutation (used for both create and
// update at a deterministic URN).
func (s *mockServer) handleUpsertPolicy(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	input, _ := variables["input"].(map[string]any)
	id := strings.TrimPrefix(urn, "urn:li:dataHubPolicy:")

	p := mockPolicy{
		ID:          id,
		Name:        asString(input["name"]),
		Type:        asString(input["type"]),
		State:       asString(input["state"]),
		Description: asString(input["description"]),
		Privileges:  asStrings(input["privileges"]),
	}
	if actors, ok := input["actors"].(map[string]any); ok {
		p.Users = asStrings(actors["users"])
		p.Groups = asStrings(actors["groups"])
		p.ResourceOwnersTypes = asStrings(actors["resourceOwnersTypes"])
		p.AllUsers, _ = actors["allUsers"].(bool)
		p.AllGroups, _ = actors["allGroups"].(bool)
		p.ResourceOwners, _ = actors["resourceOwners"].(bool)
	}
	if resources, ok := input["resources"].(map[string]any); ok {
		p.HasResources = true
		p.ResType = asString(resources["type"])
		p.ResResources = asStrings(resources["resources"])
		p.ResAll, _ = resources["allResources"].(bool)
	}

	s.mu.Lock()
	s.policies[id] = p
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"updatePolicy": urn},
	})
}

func (s *mockServer) handleDeletePolicy(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	id := strings.TrimPrefix(urn, "urn:li:dataHubPolicy:")

	s.mu.Lock()
	delete(s.policies, id)
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"deletePolicy": urn},
	})
}

func (s *mockServer) handleListPolicies(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var results []map[string]any
	for id := range s.policies {
		results = append(results, map[string]any{"urn": "urn:li:dataHubPolicy:" + id})
	}
	if results == nil {
		results = []map[string]any{}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"listPolicies": map[string]any{
				"total":    len(results),
				"policies": results,
			},
		},
	})
}

// handleDataHubPolicyItem serves GET /openapi/v3/entity/datahubpolicy/{urn}.
func (s *mockServer) handleDataHubPolicyItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/datahubpolicy/")
	id := strings.TrimPrefix(urn, "urn:li:dataHubPolicy:")

	s.mu.Lock()
	p, ok := s.policies[id]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	value := map[string]any{
		"displayName": p.Name,
		"description": p.Description,
		"type":        p.Type,
		"state":       p.State,
		"privileges":  orEmpty(p.Privileges),
		"actors": map[string]any{
			"users":               orEmpty(p.Users),
			"groups":              orEmpty(p.Groups),
			"allUsers":            p.AllUsers,
			"allGroups":           p.AllGroups,
			"resourceOwners":      p.ResourceOwners,
			"resourceOwnersTypes": orEmpty(p.ResourceOwnersTypes),
		},
	}
	if p.HasResources {
		value["resources"] = map[string]any{
			"type":         p.ResType,
			"resources":    orEmpty(p.ResResources),
			"allResources": p.ResAll,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"urn": "urn:li:dataHubPolicy:" + id,
		"dataHubPolicyKey": map[string]any{
			"value": map[string]any{"id": id},
		},
		"dataHubPolicyInfo": map[string]any{
			"value": value,
		},
	})
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asStrings(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func orEmpty(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
