// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"net/http"
	"strings"
)

const mockDataContractURNPrefix = "urn:li:dataContract:"

// mockDataContract stores the in-memory state for one data contract.
type mockDataContract struct {
	ID        string
	Entity    string
	State     string
	Freshness []string
	Schema    []string
	DataQual  []string
}

// assertionUrnsFromInput extracts the assertion URNs from a `[{assertionUrn}]`
// input list (the write shape).
func assertionUrnsFromInput(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		if m, ok := e.(map[string]any); ok {
			if u, ok := m["assertionUrn"].(string); ok && u != "" {
				out = append(out, u)
			}
		}
	}
	return out
}

func (s *mockServer) handleUpsertDataContract(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	id, _ := input["id"].(string)
	entity, _ := input["entityUrn"].(string)

	dc := mockDataContract{ID: id, Entity: entity, State: "ACTIVE"}
	if st, ok := input["state"].(string); ok && st != "" {
		dc.State = st
	}
	dc.Freshness = assertionUrnsFromInput(input["freshness"])
	dc.Schema = assertionUrnsFromInput(input["schema"])
	dc.DataQual = assertionUrnsFromInput(input["dataQuality"])

	s.mu.Lock()
	s.dataContracts[id] = dc
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"upsertDataContract": map[string]any{"urn": mockDataContractURNPrefix + id},
		},
	})
}

// refList converts stored URNs to the read shape `[{assertion: <urn>}]`.
func refList(urns []string) []map[string]any {
	out := make([]map[string]any, 0, len(urns))
	for _, u := range urns {
		out = append(out, map[string]any{"assertion": u})
	}
	return out
}

// handleDataContractItem serves GET and DELETE on
// /openapi/v3/entity/datacontract/{urn}.
func (s *mockServer) handleDataContractItem(w http.ResponseWriter, r *http.Request) {
	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/datacontract/")
	id := strings.TrimPrefix(urn, mockDataContractURNPrefix)

	switch r.Method {
	case http.MethodDelete:
		s.mu.Lock()
		delete(s.dataContracts, id)
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		return
	case http.MethodGet:
		s.mu.Lock()
		dc, ok := s.dataContracts[id]
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		props := map[string]any{"entity": dc.Entity}
		if len(dc.Freshness) > 0 {
			props["freshness"] = refList(dc.Freshness)
		}
		if len(dc.Schema) > 0 {
			props["schema"] = refList(dc.Schema)
		}
		if len(dc.DataQual) > 0 {
			props["dataQuality"] = refList(dc.DataQual)
		}
		entity := map[string]any{
			"urn":                    mockDataContractURNPrefix + id,
			"dataContractKey":        map[string]any{"value": map[string]any{"id": id}},
			"dataContractStatus":     map[string]any{"value": map[string]any{"state": dc.State}},
			"dataContractProperties": map[string]any{"value": props},
		}
		if aspect := s.structuredPropertiesAspect(mockDataContractURNPrefix + id); aspect != nil {
			entity["structuredProperties"] = aspect
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(entity)
	default:
		http.NotFound(w, r)
	}
}
