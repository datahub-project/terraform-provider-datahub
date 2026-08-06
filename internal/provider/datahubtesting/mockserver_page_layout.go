// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Mock support for page modules and page templates.
//
// The mock stores exactly what the provider sends and returns exactly that on
// read, so a round-trip defect in the provider's own conversion surfaces here
// rather than only against a live DataHub. It deliberately does NOT emulate the
// UUID-minting branch of PageModuleService/PageTemplateService: the provider is
// required to always supply a URN, and a mock that quietly invented one would
// hide a regression that breaks every rebuild.

type mockPageModule struct {
	URN    string
	ID     string
	Name   string
	Type   string
	Scope  string
	Params map[string]any
}

type mockPageTemplate struct {
	URN         string
	ID          string
	Scope       string
	SurfaceType string
	Rows        [][]string
}

func (s *mockServer) handleUpsertPageModule(w http.ResponseWriter, vars map[string]any) {
	input, _ := vars["input"].(map[string]any)
	urn, _ := input["urn"].(string)
	if urn == "" {
		writePageGraphQLError(w, "upsertPageModule called without a urn; the provider must always supply one")
		return
	}

	m := mockPageModule{
		URN:   urn,
		ID:    strings.TrimPrefix(urn, "urn:li:dataHubPageModule:"),
		Scope: "GLOBAL",
	}
	if v, ok := input["name"].(string); ok {
		m.Name = v
	}
	if v, ok := input["type"].(string); ok {
		m.Type = v
	}
	if v, ok := input["scope"].(string); ok && v != "" {
		m.Scope = v
	}
	if v, ok := input["params"].(map[string]any); ok {
		m.Params = v
	}

	s.mu.Lock()
	if s.pageModules == nil {
		s.pageModules = make(map[string]mockPageModule)
	}
	s.pageModules[urn] = m
	s.mu.Unlock()

	writePageJSON(w, map[string]any{
		"data": map[string]any{
			"upsertPageModule": map[string]any{"urn": urn},
		},
	})
}

func (s *mockServer) handleDeletePageModule(w http.ResponseWriter, vars map[string]any) {
	input, _ := vars["input"].(map[string]any)
	urn, _ := input["urn"].(string)

	s.mu.Lock()
	delete(s.pageModules, urn)
	s.mu.Unlock()

	writePageJSON(w, map[string]any{"data": map[string]any{"deletePageModule": true}})
}

func (s *mockServer) handlePageModuleItem(w http.ResponseWriter, r *http.Request) {
	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/datahubpagemodule/")

	s.mu.Lock()
	m, ok := s.pageModules[urn]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	props := map[string]any{
		"name": m.Name,
		"type": m.Type,
		"visibility": map[string]any{
			"scope": m.Scope,
		},
	}
	if m.Params != nil {
		props["params"] = m.Params
	} else {
		props["params"] = map[string]any{}
	}

	writePageJSON(w, map[string]any{
		"urn":                  m.URN,
		"dataHubPageModuleKey": map[string]any{"value": map[string]any{"id": m.ID}},
		"dataHubPageModuleProperties": map[string]any{
			"value": props,
		},
	})
}

func (s *mockServer) handleUpsertPageTemplate(w http.ResponseWriter, vars map[string]any) {
	input, _ := vars["input"].(map[string]any)
	urn, _ := input["urn"].(string)
	if urn == "" {
		writePageGraphQLError(w, "upsertPageTemplate called without a urn; the provider must always supply one")
		return
	}

	t := mockPageTemplate{
		URN:         urn,
		ID:          strings.TrimPrefix(urn, "urn:li:dataHubPageTemplate:"),
		Scope:       "GLOBAL",
		SurfaceType: "HOME_PAGE",
		Rows:        [][]string{},
	}
	if v, ok := input["scope"].(string); ok && v != "" {
		t.Scope = v
	}
	if v, ok := input["surfaceType"].(string); ok && v != "" {
		t.SurfaceType = v
	}
	if rows, ok := input["rows"].([]any); ok {
		for _, raw := range rows {
			row, _ := raw.(map[string]any)
			mods := []string{}
			if list, ok := row["modules"].([]any); ok {
				for _, mv := range list {
					if s, ok := mv.(string); ok {
						mods = append(mods, s)
					}
				}
			}
			t.Rows = append(t.Rows, mods)
		}
	}

	s.mu.Lock()
	if s.pageTemplates == nil {
		s.pageTemplates = make(map[string]mockPageTemplate)
	}
	s.pageTemplates[urn] = t
	s.mu.Unlock()

	writePageJSON(w, map[string]any{
		"data": map[string]any{
			"upsertPageTemplate": map[string]any{"urn": urn},
		},
	})
}

func (s *mockServer) handleDeletePageTemplate(w http.ResponseWriter, vars map[string]any) {
	input, _ := vars["input"].(map[string]any)
	urn, _ := input["urn"].(string)

	s.mu.Lock()
	delete(s.pageTemplates, urn)
	s.mu.Unlock()

	writePageJSON(w, map[string]any{"data": map[string]any{"deletePageTemplate": true}})
}

func (s *mockServer) handlePageTemplateItem(w http.ResponseWriter, r *http.Request) {
	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/datahubpagetemplate/")

	s.mu.Lock()
	t, ok := s.pageTemplates[urn]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	rows := make([]any, 0, len(t.Rows))
	for _, mods := range t.Rows {
		rows = append(rows, map[string]any{"modules": mods})
	}

	writePageJSON(w, map[string]any{
		"urn":                    t.URN,
		"dataHubPageTemplateKey": map[string]any{"value": map[string]any{"id": t.ID}},
		"dataHubPageTemplateProperties": map[string]any{
			"value": map[string]any{
				"rows":       rows,
				"surface":    map[string]any{"surfaceType": t.SurfaceType},
				"visibility": map[string]any{"scope": t.Scope},
			},
		},
	})
}

func writePageJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// writePageGraphQLError reports a mock-side contract violation as a GraphQL
// error, so the failing test names the cause rather than surfacing a nil.
func writePageGraphQLError(w http.ResponseWriter, msg string) {
	writePageJSON(w, map[string]any{
		"errors": []map[string]any{{"message": msg}},
	})
}
