// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"net/http"
	"strings"
)

// mockUser mirrors the corpUser shape the provider reads. NativeGroups reflects
// the nativeGroupMembership aspect (maintained by group-member mutations).
// RoleURN reflects the single roleMembership entry (maintained by role
// assignment mutations).
type mockUser struct {
	URN          string
	Username     string
	DisplayName  string
	Email        string
	Title        string
	Active       bool
	Status       string
	NativeGroups []string
	RoleURN      string
}

// seedUsers pre-populates a couple of users so corp_user lookups, group
// membership, and role assignment scenarios have real actors to reference.
// "testuser" matches the identity returned by handleMe.
func (s *mockServer) seedUsers() {
	s.users["testuser"] = mockUser{
		URN:         "urn:li:corpuser:testuser",
		Username:    "testuser",
		DisplayName: "Test User",
		Email:       "testuser@example.com",
		Title:       "Engineer",
		Active:      true,
		Status:      "ACTIVE",
	}
	s.users["datahub"] = mockUser{
		URN:         "urn:li:corpuser:datahub",
		Username:    "datahub",
		DisplayName: "DataHub",
		Active:      true,
	}
}

// handleCorpUserItem serves GET /openapi/v3/entity/corpuser/{urn}, returning the
// same aspect shape as the real OpenAPI v3 endpoint.
func (s *mockServer) handleCorpUserItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/corpuser/")
	username := strings.TrimPrefix(urn, "urn:li:corpuser:")

	s.mu.Lock()
	u, ok := s.users[username]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	entity := map[string]any{
		"urn": u.URN,
		"corpUserKey": map[string]any{
			"value": map[string]any{"username": u.Username},
		},
		"corpUserInfo": map[string]any{
			"value": map[string]any{
				"displayName": u.DisplayName,
				"email":       u.Email,
				"title":       u.Title,
				"active":      u.Active,
			},
		},
	}
	if u.Status != "" {
		entity["corpUserStatus"] = map[string]any{
			"value": map[string]any{"status": u.Status},
		}
	}
	if len(u.NativeGroups) > 0 {
		entity["nativeGroupMembership"] = map[string]any{
			"value": map[string]any{"nativeGroups": u.NativeGroups},
		}
	}
	if u.RoleURN != "" {
		entity["roleMembership"] = map[string]any{
			"value": map[string]any{"roles": []string{u.RoleURN}},
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entity)
}
