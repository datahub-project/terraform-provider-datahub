// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// mockOwnershipType mirrors the ownership type shape the provider sends and reads.
type mockOwnershipType struct {
	URN         string
	ID          string
	Name        string
	Description string
}

// handleOwnershipTypeCollection handles POST /openapi/v3/entity/ownershiptype
// (no trailing slash). The provider uses this to write the ownershipTypeInfo
// aspect on create and update.
func (s *mockServer) handleOwnershipTypeCollection(w http.ResponseWriter, r *http.Request) {
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
		id := strings.TrimPrefix(urn, "urn:li:ownershipType:")
		infoRaw, ok := entity["ownershipTypeInfo"].(map[string]any)
		if !ok {
			continue
		}
		valueRaw, _ := infoRaw["value"].(map[string]any)
		name, _ := valueRaw["name"].(string)
		desc, _ := valueRaw["description"].(string)
		s.ownershipTypes[id] = mockOwnershipType{
			URN:         urn,
			ID:          id,
			Name:        name,
			Description: desc,
		}
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// handleOwnershipTypeItem serves GET /openapi/v3/entity/ownershiptype/{urn},
// returning the same aspect shape as the real OpenAPI v3 endpoint.
func (s *mockServer) handleOwnershipTypeItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/ownershiptype/")
	id := strings.TrimPrefix(urn, "urn:li:ownershipType:")

	s.mu.Lock()
	ot, ok := s.ownershipTypes[id]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	entity := map[string]any{
		"urn": ot.URN,
		"ownershipTypeKey": map[string]any{
			"value": map[string]any{"id": ot.ID},
		},
		"ownershipTypeInfo": map[string]any{
			"value": map[string]any{
				"name":        ot.Name,
				"description": ot.Description,
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entity)
}

// handleListOwnershipTypes serves the listOwnershipTypes GraphQL query.
func (s *mockServer) handleListOwnershipTypes(w http.ResponseWriter, _ map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var ownershipTypes []map[string]any
	for _, ot := range s.ownershipTypes {
		ownershipTypes = append(ownershipTypes, map[string]any{
			"urn": ot.URN,
		})
	}
	if ownershipTypes == nil {
		ownershipTypes = []map[string]any{}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"listOwnershipTypes": map[string]any{
				"total":          len(ownershipTypes),
				"ownershipTypes": ownershipTypes,
			},
		},
	})
}

// handleDeleteOwnershipType serves the deleteOwnershipType GraphQL mutation.
func (s *mockServer) handleDeleteOwnershipType(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	id := strings.TrimPrefix(urn, "urn:li:ownershipType:")

	s.mu.Lock()
	delete(s.ownershipTypes, id)
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"deleteOwnershipType": true},
	})
}
