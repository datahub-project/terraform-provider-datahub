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
	URN              string
	ID               string
	Name             string
	Definition       string // maps to "description" in the Terraform schema
	ParentNode       string // full glossaryNode URN or ""
	Domain           string // full domain URN or ""
	CustomProperties map[string]string
}

// mockGlossaryTerm mirrors the glossary term shape the provider sends and reads.
type mockGlossaryTerm struct {
	URN              string
	ID               string
	Name             string
	Definition       string // maps to "description" in the Terraform schema
	ParentNode       string // full glossaryNode URN or ""
	Domain           string // full domain URN or ""
	CustomProperties map[string]string
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

// handleSetDomain handles the setDomain mutation for glossary nodes and terms.
// The real mutation is generic and supports any entity type; this mock
// dispatches by URN prefix to update the correct entity store.
func (s *mockServer) handleSetDomain(w http.ResponseWriter, variables map[string]any) {
	entityURN, _ := variables["entityUrn"].(string)
	domainURN, _ := variables["domainUrn"].(string)

	s.mu.Lock()
	switch {
	case strings.HasPrefix(entityURN, "urn:li:glossaryNode:"):
		id := strings.TrimPrefix(entityURN, "urn:li:glossaryNode:")
		if n, ok := s.glossaryNodes[id]; ok {
			n.Domain = domainURN
			s.glossaryNodes[id] = n
		}
	case strings.HasPrefix(entityURN, "urn:li:glossaryTerm:"):
		id := strings.TrimPrefix(entityURN, "urn:li:glossaryTerm:")
		if t, ok := s.glossaryTerms[id]; ok {
			t.Domain = domainURN
			s.glossaryTerms[id] = t
		}
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"setDomain": true},
	})
}

// handleUnsetDomain handles the unsetDomain mutation for glossary nodes and terms.
func (s *mockServer) handleUnsetDomain(w http.ResponseWriter, variables map[string]any) {
	entityURN, _ := variables["entityUrn"].(string)

	s.mu.Lock()
	switch {
	case strings.HasPrefix(entityURN, "urn:li:glossaryNode:"):
		id := strings.TrimPrefix(entityURN, "urn:li:glossaryNode:")
		if n, ok := s.glossaryNodes[id]; ok {
			n.Domain = ""
			s.glossaryNodes[id] = n
		}
	case strings.HasPrefix(entityURN, "urn:li:glossaryTerm:"):
		id := strings.TrimPrefix(entityURN, "urn:li:glossaryTerm:")
		if t, ok := s.glossaryTerms[id]; ok {
			t.Domain = ""
			s.glossaryTerms[id] = t
		}
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"unsetDomain": true},
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

	nodeInfoValue := map[string]any{
		"name":       n.Name,
		"definition": n.Definition,
		"parentNode": n.ParentNode,
	}
	if len(n.CustomProperties) > 0 {
		nodeInfoValue["customProperties"] = n.CustomProperties
	}
	entity := map[string]any{
		"urn": n.URN,
		"glossaryNodeKey": map[string]any{
			"value": map[string]any{"name": n.ID},
		},
		"glossaryNodeInfo": map[string]any{
			"value": nodeInfoValue,
		},
	}
	if n.Domain != "" {
		entity["domains"] = map[string]any{
			"value": map[string]any{"domains": []string{n.Domain}},
		}
	}
	if aspect := s.structuredPropertiesAspect(n.URN); aspect != nil {
		entity["structuredProperties"] = aspect
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entity)
}

// parseAspectCustomProperties extracts the customProperties map from a decoded
// aspect value (map[string]any of string->string). Returns nil when absent.
func parseAspectCustomProperties(val map[string]any) map[string]string {
	cp, ok := val["customProperties"].(map[string]any)
	if !ok {
		return nil
	}
	m := make(map[string]string, len(cp))
	for k, v := range cp {
		m[k], _ = v.(string)
	}
	return m
}

// handleGlossaryNodeWrite serves POST /openapi/v3/entity/glossarynode, the aspect
// write the provider uses to set customProperties (which the GraphQL
// createGlossaryNode mutation does not carry). It replaces the glossaryNodeInfo
// aspect from the payload, mirroring the real OpenAPI v3 semantics - so if the
// provider ever omitted name/definition from this write, the stored values would
// be clobbered and the clobber-guard test would catch it. The domains aspect is
// written separately (setDomain), so the existing Domain is preserved here.
func (s *mockServer) handleGlossaryNodeWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var entities []map[string]any
	if err := json.NewDecoder(r.Body).Decode(&entities); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	for _, e := range entities {
		urn, _ := e["urn"].(string)
		id := strings.TrimPrefix(urn, "urn:li:glossaryNode:")
		info, _ := e["glossaryNodeInfo"].(map[string]any)
		val, _ := info["value"].(map[string]any)

		n := s.glossaryNodes[id] // preserve Domain (set via setDomain, not this write)
		n.URN = urn
		n.ID = id
		n.Name, _ = val["name"].(string)
		n.Definition, _ = val["definition"].(string)
		n.ParentNode, _ = val["parentNode"].(string)
		n.CustomProperties = parseAspectCustomProperties(val)
		s.glossaryNodes[id] = n
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entities)
}

// handleGlossaryTermWrite serves POST /openapi/v3/entity/glossaryterm, the aspect
// write the provider uses to set customProperties on a term. Mirrors
// handleGlossaryNodeWrite; the domains aspect is preserved.
func (s *mockServer) handleGlossaryTermWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var entities []map[string]any
	if err := json.NewDecoder(r.Body).Decode(&entities); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	for _, e := range entities {
		urn, _ := e["urn"].(string)
		id := strings.TrimPrefix(urn, "urn:li:glossaryTerm:")
		info, _ := e["glossaryTermInfo"].(map[string]any)
		val, _ := info["value"].(map[string]any)

		t := s.glossaryTerms[id] // preserve Domain (set via setDomain, not this write)
		t.URN = urn
		t.ID = id
		t.Name, _ = val["name"].(string)
		t.Definition, _ = val["definition"].(string)
		t.ParentNode, _ = val["parentNode"].(string)
		t.CustomProperties = parseAspectCustomProperties(val)
		s.glossaryTerms[id] = t
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entities)
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

	termInfoValue := map[string]any{
		"name":       t.Name,
		"definition": t.Definition,
		"parentNode": t.ParentNode,
	}
	if len(t.CustomProperties) > 0 {
		termInfoValue["customProperties"] = t.CustomProperties
	}
	entity := map[string]any{
		"urn": t.URN,
		"glossaryTermKey": map[string]any{
			"value": map[string]any{"name": t.ID},
		},
		"glossaryTermInfo": map[string]any{
			"value": termInfoValue,
		},
	}
	if t.Domain != "" {
		entity["domains"] = map[string]any{
			"value": map[string]any{"domains": []string{t.Domain}},
		}
	}
	if aspect := s.structuredPropertiesAspect(t.URN); aspect != nil {
		entity["structuredProperties"] = aspect
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entity)
}
