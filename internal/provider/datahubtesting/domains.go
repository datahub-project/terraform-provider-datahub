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
	URN              string
	ID               string
	Name             string
	Description      string
	ParentDomain     string // full URN or ""
	CustomProperties map[string]string
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

// handleUpdateDescription handles the generic updateDescription mutation. The
// real mutation supports multiple entity types; this mock dispatches by URN
// prefix to update the correct entity store.
func (s *mockServer) handleUpdateDescription(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	urn, _ := input["resourceUrn"].(string)
	desc, _ := input["description"].(string)

	s.mu.Lock()
	switch {
	case strings.HasPrefix(urn, "urn:li:domain:"):
		id := strings.TrimPrefix(urn, "urn:li:domain:")
		if d, ok := s.domains[id]; ok {
			d.Description = desc
			s.domains[id] = d
		}
	case strings.HasPrefix(urn, "urn:li:glossaryNode:"):
		id := strings.TrimPrefix(urn, "urn:li:glossaryNode:")
		if n, ok := s.glossaryNodes[id]; ok {
			n.Definition = desc
			s.glossaryNodes[id] = n
		}
	case strings.HasPrefix(urn, "urn:li:glossaryTerm:"):
		id := strings.TrimPrefix(urn, "urn:li:glossaryTerm:")
		if t, ok := s.glossaryTerms[id]; ok {
			t.Definition = desc
			s.glossaryTerms[id] = t
		}
	case strings.HasPrefix(urn, "urn:li:tag:"):
		id := strings.TrimPrefix(urn, "urn:li:tag:")
		if t, ok := s.tags[id]; ok {
			t.Description = desc
			s.tags[id] = t
		}
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

	propsValue := map[string]any{
		"name":         d.Name,
		"description":  d.Description,
		"parentDomain": d.ParentDomain,
	}
	if len(d.CustomProperties) > 0 {
		propsValue["customProperties"] = d.CustomProperties
	}
	entity := map[string]any{
		"urn": d.URN,
		"domainKey": map[string]any{
			"value": map[string]any{"id": d.ID},
		},
		"domainProperties": map[string]any{
			"value": propsValue,
		},
	}
	if aspect := s.structuredPropertiesAspect(d.URN); aspect != nil {
		entity["structuredProperties"] = aspect
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entity)
}

// handleDomainWrite serves POST /openapi/v3/entity/domain, the aspect write the
// provider uses to set customProperties (which the GraphQL createDomain mutation
// does not carry). It replaces the whole domainProperties aspect from the
// payload, mirroring the real OpenAPI v3 semantics - so if the provider ever
// omitted name/description/parentDomain from this write, the stored values would
// be clobbered and the clobber-guard test would catch it.
func (s *mockServer) handleDomainWrite(w http.ResponseWriter, r *http.Request) {
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
		id := strings.TrimPrefix(urn, "urn:li:domain:")
		props, _ := e["domainProperties"].(map[string]any)
		val, _ := props["value"].(map[string]any)

		d := s.domains[id]
		d.URN = urn
		d.ID = id
		d.Name, _ = val["name"].(string)
		d.Description, _ = val["description"].(string)
		d.ParentDomain, _ = val["parentDomain"].(string)
		if cp, ok := val["customProperties"].(map[string]any); ok {
			m := make(map[string]string, len(cp))
			for k, v := range cp {
				m[k], _ = v.(string)
			}
			d.CustomProperties = m
		} else {
			d.CustomProperties = nil
		}
		s.domains[id] = d
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entities)
}
