// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"net/http"
	"strings"
)

// mockGlossaryNode mirrors the glossary node shape the provider sends and reads.
type mockGlossaryNode struct {
	URN        string
	ID         string
	Name       string
	Definition string // maps to "description" in the Terraform schema
	ParentNode string // full glossaryNode URN or ""
}

// mockGlossaryTerm mirrors the glossary term shape the provider sends and reads.
type mockGlossaryTerm struct {
	URN        string
	ID         string
	Name       string
	Definition string // maps to "description" in the Terraform schema
	ParentNode string // full glossaryNode URN or ""
}

// handleCreateGlossaryNode handles the createGlossaryNode mutation.
func (s *mockServer) handleCreateGlossaryNode(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	id, _ := input["id"].(string)
	name, _ := input["name"].(string)
	def, _ := input["description"].(string)
	parent, _ := input["parentNode"].(string)
	urn := "urn:li:glossaryNode:" + id

	s.mu.Lock()
	s.glossaryNodes[id] = mockGlossaryNode{URN: urn, ID: id, Name: name, Definition: def, ParentNode: parent}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"createGlossaryNode": urn},
	})
}

// handleCreateGlossaryTerm handles the createGlossaryTerm mutation.
func (s *mockServer) handleCreateGlossaryTerm(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	id, _ := input["id"].(string)
	name, _ := input["name"].(string)
	def, _ := input["description"].(string)
	parent, _ := input["parentNode"].(string)
	urn := "urn:li:glossaryTerm:" + id

	s.mu.Lock()
	s.glossaryTerms[id] = mockGlossaryTerm{URN: urn, ID: id, Name: name, Definition: def, ParentNode: parent}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"createGlossaryTerm": urn},
	})
}

// handleUpdateParentNode handles the updateParentNode mutation. A null
// parentNode detaches the entity from its parent (moves to root level).
func (s *mockServer) handleUpdateParentNode(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	urn, _ := input["resourceUrn"].(string)
	// parentNode may be nil (JSON null) or a string.
	newParent, _ := input["parentNode"].(string) // "" if null or absent

	s.mu.Lock()
	switch {
	case strings.HasPrefix(urn, "urn:li:glossaryNode:"):
		id := strings.TrimPrefix(urn, "urn:li:glossaryNode:")
		if n, ok := s.glossaryNodes[id]; ok {
			n.ParentNode = newParent
			s.glossaryNodes[id] = n
		}
	case strings.HasPrefix(urn, "urn:li:glossaryTerm:"):
		id := strings.TrimPrefix(urn, "urn:li:glossaryTerm:")
		if t, ok := s.glossaryTerms[id]; ok {
			t.ParentNode = newParent
			s.glossaryTerms[id] = t
		}
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"updateParentNode": true},
	})
}

// handleDeleteGlossaryEntity handles the deleteGlossaryEntity mutation for
// both nodes and terms. Unlike deleteDomain, there is no child guard -- the
// mock deletes unconditionally, matching the real server's behavior.
func (s *mockServer) handleDeleteGlossaryEntity(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)

	s.mu.Lock()
	switch {
	case strings.HasPrefix(urn, "urn:li:glossaryNode:"):
		id := strings.TrimPrefix(urn, "urn:li:glossaryNode:")
		delete(s.glossaryNodes, id)
	case strings.HasPrefix(urn, "urn:li:glossaryTerm:"):
		id := strings.TrimPrefix(urn, "urn:li:glossaryTerm:")
		delete(s.glossaryTerms, id)
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"deleteGlossaryEntity": true},
	})
}

// handleGlossaryNodeItem serves GET /openapi/v3/entity/glossarynode/{urn},
// returning the same aspect shape as the real OpenAPI v3 endpoint.
func (s *mockServer) handleGlossaryNodeItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/glossarynode/")
	id := strings.TrimPrefix(urn, "urn:li:glossaryNode:")

	s.mu.Lock()
	n, ok := s.glossaryNodes[id]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	entity := map[string]any{
		"urn": n.URN,
		"glossaryNodeKey": map[string]any{
			"value": map[string]any{"name": n.ID},
		},
		"glossaryNodeInfo": map[string]any{
			"value": map[string]any{
				"name":       n.Name,
				"definition": n.Definition,
				"parentNode": n.ParentNode,
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entity)
}

// handleGlossaryTermItem serves GET /openapi/v3/entity/glossaryterm/{urn},
// returning the same aspect shape as the real OpenAPI v3 endpoint.
func (s *mockServer) handleGlossaryTermItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/glossaryterm/")
	id := strings.TrimPrefix(urn, "urn:li:glossaryTerm:")

	s.mu.Lock()
	t, ok := s.glossaryTerms[id]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	entity := map[string]any{
		"urn": t.URN,
		"glossaryTermKey": map[string]any{
			"value": map[string]any{"name": t.ID},
		},
		"glossaryTermInfo": map[string]any{
			"value": map[string]any{
				"name":       t.Name,
				"definition": t.Definition,
				"parentNode": t.ParentNode,
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entity)
}
