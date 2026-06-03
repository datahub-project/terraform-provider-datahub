// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// mockDomain mirrors the domain shape the provider sends and reads.
type mockDomain struct {
	URN          string
	ID           string
	Name         string
	Description  string
	ParentDomain string // full URN or ""
}

// handleCreateDomain handles the createDomain mutation. DataHub's real
// createDomain overwrites an existing domain when the same id is re-used; the
// mock does the same.
func (s *mockServer) handleCreateDomain(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	id, _ := input["id"].(string)
	name, _ := input["name"].(string)
	desc, _ := input["description"].(string)
	parent, _ := input["parentDomain"].(string)
	urn := "urn:li:domain:" + id

	s.mu.Lock()
	s.domains[id] = mockDomain{URN: urn, ID: id, Name: name, Description: desc, ParentDomain: parent}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"createDomain": urn},
	})
}

// handleMoveDomain handles the moveDomain mutation. A null parentDomain
// promotes the domain to root (removes any existing parent).
func (s *mockServer) handleMoveDomain(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	urn, _ := input["resourceUrn"].(string)
	// parentDomain may be nil (JSON null) or a string.
	newParent, _ := input["parentDomain"].(string) // "" if null or absent
	id := strings.TrimPrefix(urn, "urn:li:domain:")

	s.mu.Lock()
	if d, ok := s.domains[id]; ok {
		d.ParentDomain = newParent
		s.domains[id] = d
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"moveDomain": true},
	})
}

// handleDeleteDomain handles the deleteDomain mutation. Replicates the real
// server's child-guard: refuses deletion if any stored domain has this URN as
// its parent, matching DataHub's "Cannot delete domain %s which has child domains" error.
func (s *mockServer) handleDeleteDomain(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	id := strings.TrimPrefix(urn, "urn:li:domain:")

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, d := range s.domains {
		if d.ParentDomain == urn {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]any{{
					"message": fmt.Sprintf("Cannot delete domain %s which has child domains", urn),
				}},
				"data": map[string]any{"deleteDomain": nil},
			})
			return
		}
	}

	delete(s.domains, id)

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"deleteDomain": true},
	})
}

// handleUpdateDescription handles the updateDescription mutation for domains.
// The real mutation is generic (supports multiple entity types); the mock
// dispatches here when the resourceUrn is a domain URN.
func (s *mockServer) handleUpdateDescription(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	urn, _ := input["resourceUrn"].(string)
	desc, _ := input["description"].(string)
	id := strings.TrimPrefix(urn, "urn:li:domain:")

	s.mu.Lock()
	if d, ok := s.domains[id]; ok {
		d.Description = desc
		s.domains[id] = d
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"updateDescription": true},
	})
}

// handleDomainItem serves GET /openapi/v3/entity/domain/{urn}, returning the
// same aspect shape as the real OpenAPI v3 endpoint.
func (s *mockServer) handleDomainItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/domain/")
	id := strings.TrimPrefix(urn, "urn:li:domain:")

	s.mu.Lock()
	d, ok := s.domains[id]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	entity := map[string]any{
		"urn": d.URN,
		"domainKey": map[string]any{
			"value": map[string]any{"id": d.ID},
		},
		"domainProperties": map[string]any{
			"value": map[string]any{
				"name":         d.Name,
				"description":  d.Description,
				"parentDomain": d.ParentDomain,
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entity)
}
