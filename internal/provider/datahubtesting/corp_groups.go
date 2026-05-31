// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"net/http"
	"strings"
)

// mockGroup mirrors the corpGroup shape the provider sends and reads.
// Name is the corpGroupInfo.displayName; Description/Email/Slack are the
// corpGroupEditableInfo fields.
type mockGroup struct {
	URN         string
	ID          string
	Name        string
	Description string
	Email       string
	Slack       string
}

// handleCreateGroup handles the createGroup mutation. Like the real server it is
// create-only: a second create for the same id returns "This Group already exists!".
func (s *mockServer) handleCreateGroup(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	id, _ := input["id"].(string)
	name, _ := input["name"].(string)
	urn := "urn:li:corpGroup:" + id

	s.mu.Lock()
	_, exists := s.groups[id]
	if !exists {
		s.groups[id] = mockGroup{URN: urn, ID: id, Name: name}
	}
	s.mu.Unlock()

	if exists {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errors": []map[string]any{{"message": "This Group already exists!"}},
			"data":   map[string]any{"createGroup": nil},
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"createGroup": urn},
	})
}

// handleUpdateName updates a group's display name (corpGroupInfo.displayName).
func (s *mockServer) handleUpdateName(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	urn, _ := input["urn"].(string)
	name, _ := input["name"].(string)
	id := strings.TrimPrefix(urn, "urn:li:corpGroup:")

	s.mu.Lock()
	if g, ok := s.groups[id]; ok {
		g.Name = name
		s.groups[id] = g
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"updateName": true},
	})
}

// handleUpdateCorpGroupProperties updates the editable group properties.
func (s *mockServer) handleUpdateCorpGroupProperties(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	input, _ := variables["input"].(map[string]any)
	desc, _ := input["description"].(string)
	email, _ := input["email"].(string)
	slack, _ := input["slack"].(string)
	id := strings.TrimPrefix(urn, "urn:li:corpGroup:")

	s.mu.Lock()
	if g, ok := s.groups[id]; ok {
		g.Description = desc
		g.Email = email
		g.Slack = slack
		s.groups[id] = g
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"updateCorpGroupProperties": map[string]any{"urn": urn}},
	})
}

// handleRemoveGroup deletes a group.
func (s *mockServer) handleRemoveGroup(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	id := strings.TrimPrefix(urn, "urn:li:corpGroup:")

	s.mu.Lock()
	delete(s.groups, id)
	// Drop the group from any user's native membership.
	for username, u := range s.users {
		filtered := u.NativeGroups[:0:0]
		for _, g := range u.NativeGroups {
			if g != urn {
				filtered = append(filtered, g)
			}
		}
		u.NativeGroups = filtered
		s.users[username] = u
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"removeGroup": true},
	})
}

// handleListGroups returns the URNs of all groups.
func (s *mockServer) handleListGroups(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var results []map[string]any
	for _, g := range s.groups {
		results = append(results, map[string]any{"urn": g.URN})
	}
	if results == nil {
		results = []map[string]any{}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"listGroups": map[string]any{
				"total":  len(results),
				"groups": results,
			},
		},
	})
}

// handleCorpGroupItem serves GET /openapi/v3/entity/corpgroup/{urn}, returning
// the same aspect shape as the real OpenAPI v3 endpoint.
func (s *mockServer) handleCorpGroupItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/corpgroup/")
	id := strings.TrimPrefix(urn, "urn:li:corpGroup:")

	s.mu.Lock()
	g, ok := s.groups[id]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"urn": g.URN,
		"corpGroupKey": map[string]any{
			"value": map[string]any{"name": g.ID},
		},
		"corpGroupInfo": map[string]any{
			"value": map[string]any{"displayName": g.Name},
		},
		"corpGroupEditableInfo": map[string]any{
			"value": map[string]any{
				"description": g.Description,
				"email":       g.Email,
				"slack":       g.Slack,
			},
		},
	})
}
