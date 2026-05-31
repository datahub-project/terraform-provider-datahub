// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"net/http"
	"strings"
)

// handleAddGroupMembers reflects addGroupMembers into each user's
// nativeGroupMembership, mirroring where the real server stores membership.
func (s *mockServer) handleAddGroupMembers(w http.ResponseWriter, variables map[string]any) {
	groupURN, userURNs := groupAndUsers(variables)

	s.mu.Lock()
	for _, userURN := range userURNs {
		username := strings.TrimPrefix(userURN, "urn:li:corpuser:")
		u, ok := s.users[username]
		if !ok {
			u = mockUser{URN: userURN, Username: username, Active: true}
		}
		if !contains(u.NativeGroups, groupURN) {
			u.NativeGroups = append(u.NativeGroups, groupURN)
		}
		s.users[username] = u
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"addGroupMembers": true},
	})
}

// handleRemoveGroupMembers removes the group from each user's
// nativeGroupMembership.
func (s *mockServer) handleRemoveGroupMembers(w http.ResponseWriter, variables map[string]any) {
	groupURN, userURNs := groupAndUsers(variables)

	s.mu.Lock()
	for _, userURN := range userURNs {
		username := strings.TrimPrefix(userURN, "urn:li:corpuser:")
		u, ok := s.users[username]
		if !ok {
			continue
		}
		filtered := u.NativeGroups[:0:0]
		for _, g := range u.NativeGroups {
			if g != groupURN {
				filtered = append(filtered, g)
			}
		}
		u.NativeGroups = filtered
		s.users[username] = u
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"removeGroupMembers": true},
	})
}

// groupAndUsers extracts groupUrn and userUrns from a membership mutation's
// input variables.
func groupAndUsers(variables map[string]any) (string, []string) {
	input, _ := variables["input"].(map[string]any)
	groupURN, _ := input["groupUrn"].(string)
	rawUsers, _ := input["userUrns"].([]any)
	users := make([]string, 0, len(rawUsers))
	for _, u := range rawUsers {
		if s, ok := u.(string); ok {
			users = append(users, s)
		}
	}
	return groupURN, users
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
