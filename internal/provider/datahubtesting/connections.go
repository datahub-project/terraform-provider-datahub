// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"net/http"
	"strings"
)

// mockConnection mirrors the shape the provider sends and reads for connections.
// The blob is stored as-is (not encrypted in tests, unlike the real server).
type mockConnection struct {
	URN      string
	ID       string
	Name     string
	Platform string // full platform URN (e.g., "urn:li:dataPlatform:databricks")
	Blob     string
}

// handleCreateOrUpdateConnection handles upsertConnection GraphQL mutations.
func (s *mockServer) handleCreateOrUpdateConnection(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	id, _ := input["id"].(string)
	name, _ := input["name"].(string)
	platformURN, _ := input["platformUrn"].(string)

	jsonBlock, _ := input["json"].(map[string]any)
	blob, _ := jsonBlock["blob"].(string)

	if id == "" {
		id = strings.ReplaceAll(strings.ToLower(name), " ", "-")
	}
	urnVal := "urn:li:dataHubConnection:" + id

	s.mu.Lock()
	s.connections[id] = mockConnection{
		URN:      urnVal,
		ID:       id,
		Name:     name,
		Platform: platformURN,
		Blob:     blob,
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"upsertConnection": map[string]any{
				"urn": urnVal,
			},
		},
	})
}

// handleConnectionItem serves GET and DELETE on
// /openapi/v3/entity/datahubconnection/{urn}.
// The blob is intentionally NOT included in GET responses to match the real
// server's behaviour (where it is encrypted and unavailable to the caller).
func (s *mockServer) handleConnectionItem(w http.ResponseWriter, r *http.Request) {
	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/datahubconnection/")
	id := strings.TrimPrefix(urn, "urn:li:dataHubConnection:")

	switch r.Method {
	case http.MethodDelete:
		s.mu.Lock()
		delete(s.connections, id)
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)

	case http.MethodGet:
		s.mu.Lock()
		conn, ok := s.connections[id]
		s.mu.Unlock()

		if !ok {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"urn": conn.URN,
			"dataHubConnectionKey": map[string]any{
				"value": map[string]any{"id": conn.ID},
			},
			"dataHubConnectionDetails": map[string]any{
				"value": map[string]any{
					"name":     conn.Name,
					"type":     "JSON",
					"platform": conn.Platform,
					// blob deliberately omitted: encrypted at rest in real DataHub
				},
			},
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
