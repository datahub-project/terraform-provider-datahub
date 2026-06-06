// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// mockTag mirrors the tag shape the provider sends and reads.
type mockTag struct {
	URN         string
	ID          string
	Name        string
	Description string
	ColorHex    string // "#RRGGBB" or ""
}

// handleCreateTag handles the createTag GraphQL mutation.
func (s *mockServer) handleCreateTag(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	id, _ := input["id"].(string)
	name, _ := input["name"].(string)
	desc, _ := input["description"].(string)
	urn := "urn:li:tag:" + id

	s.mu.Lock()
	s.tags[id] = mockTag{URN: urn, ID: id, Name: name, Description: desc}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"createTag": urn},
	})
}

// handleSetTagColor handles the setTagColor GraphQL mutation.
func (s *mockServer) handleSetTagColor(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	colorHex, _ := variables["colorHex"].(string)
	id := strings.TrimPrefix(urn, "urn:li:tag:")

	s.mu.Lock()
	if t, ok := s.tags[id]; ok {
		t.ColorHex = colorHex
		s.tags[id] = t
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"setTagColor": true},
	})
}

// handleDeleteTag handles the deleteTag GraphQL mutation.
func (s *mockServer) handleDeleteTag(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	id := strings.TrimPrefix(urn, "urn:li:tag:")

	s.mu.Lock()
	delete(s.tags, id)
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"deleteTag": true},
	})
}

// handleTagCollection handles POST /openapi/v3/entity/tag (no trailing slash),
// which the provider uses to write the tagProperties aspect (rename path).
// Body is an array of entity objects; each may carry a "tagProperties" key.
func (s *mockServer) handleTagCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, _ := io.ReadAll(r.Body)
	var entities []map[string]any
	if err := json.Unmarshal(body, &entities); err != nil || len(entities) == 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	for _, entity := range entities {
		urn, _ := entity["urn"].(string)
		if urn == "" {
			continue
		}
		id := strings.TrimPrefix(urn, "urn:li:tag:")
		propsRaw, ok := entity["tagProperties"].(map[string]any)
		if !ok {
			continue
		}
		valueRaw, _ := propsRaw["value"].(map[string]any)
		name, _ := valueRaw["name"].(string)
		desc, _ := valueRaw["description"].(string)
		color, _ := valueRaw["colorHex"].(string)
		if t, exists := s.tags[id]; exists {
			if name != "" {
				t.Name = name
			}
			t.Description = desc
			t.ColorHex = color
			s.tags[id] = t
		}
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// handleTagItem serves GET /openapi/v3/entity/tag/{urn}, returning the same
// aspect shape as the real OpenAPI v3 endpoint.
func (s *mockServer) handleTagItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/tag/")
	id := strings.TrimPrefix(urn, "urn:li:tag:")

	s.mu.Lock()
	t, ok := s.tags[id]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	propsValue := map[string]any{
		"name":        t.Name,
		"description": t.Description,
		"colorHex":    t.ColorHex,
	}

	entity := map[string]any{
		"urn": t.URN,
		"tagKey": map[string]any{
			"value": map[string]any{"name": t.ID},
		},
		"tagProperties": map[string]any{
			"value": propsValue,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entity)
}
