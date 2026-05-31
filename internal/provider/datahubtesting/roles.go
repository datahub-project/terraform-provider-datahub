// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"net/http"
	"strings"
)

// builtInRoles mirrors the three roles DataHub seeds at bootstrap.
var builtInRoles = map[string]struct {
	Name        string
	Description string
}{
	"Admin":  {Name: "Admin", Description: "Can do everything on the platform."},
	"Editor": {Name: "Editor", Description: "Can read and edit all metadata."},
	"Reader": {Name: "Reader", Description: "Can read all metadata."},
}

// handleListRoles returns the URNs of the built-in roles.
func (s *mockServer) handleListRoles(w http.ResponseWriter) {
	roles := []map[string]any{
		{"urn": "urn:li:dataHubRole:Admin"},
		{"urn": "urn:li:dataHubRole:Editor"},
		{"urn": "urn:li:dataHubRole:Reader"},
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"listRoles": map[string]any{
				"total": len(roles),
				"roles": roles,
			},
		},
	})
}

// handleBatchAssignRole sets or clears the single roleMembership entry on each
// actor (user or group). A missing roleUrn clears the role.
func (s *mockServer) handleBatchAssignRole(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	roleURN, _ := input["roleUrn"].(string)
	rawActors, _ := input["actors"].([]any)

	s.mu.Lock()
	for _, a := range rawActors {
		actorURN, ok := a.(string)
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(actorURN, "urn:li:corpGroup:"):
			id := strings.TrimPrefix(actorURN, "urn:li:corpGroup:")
			if g, ok := s.groups[id]; ok {
				g.RoleURN = roleURN
				s.groups[id] = g
			}
		case strings.HasPrefix(actorURN, "urn:li:corpuser:"):
			username := strings.TrimPrefix(actorURN, "urn:li:corpuser:")
			u, ok := s.users[username]
			if !ok {
				u = mockUser{URN: actorURN, Username: username, Active: true}
			}
			u.RoleURN = roleURN
			s.users[username] = u
		}
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"batchAssignRole": true},
	})
}

// handleDataHubRoleItem serves GET /openapi/v3/entity/datahubrole/{urn} for the
// built-in roles.
func (s *mockServer) handleDataHubRoleItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/datahubrole/")
	name := strings.TrimPrefix(urn, "urn:li:dataHubRole:")

	role, ok := builtInRoles[name]
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"urn": "urn:li:dataHubRole:" + name,
		"dataHubRoleKey": map[string]any{
			"value": map[string]any{"id": name},
		},
		"dataHubRoleInfo": map[string]any{
			"value": map[string]any{
				"name":        role.Name,
				"description": role.Description,
				"editable":    false,
			},
		},
	})
}
