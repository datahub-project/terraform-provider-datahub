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
	urnVal, _ := input["urn"].(string)
	name, _ := input["name"].(string)
	platform, _ := input["platform"].(string)

	details, _ := input["details"].(map[string]any)
	blob, _ := details["blob"].(string)

	// Derive ID from URN or generate one from name.
	id := strings.TrimPrefix(urnVal, "urn:li:dataHubConnection:")
	if id == "" || id == urnVal {
		// No URN provided on create: use name as fallback.
		id = strings.ReplaceAll(strings.ToLower(name), " ", "-")
	}
	if urnVal == "" {
		urnVal = "urn:li:dataHubConnection:" + id
	}

	s.mu.Lock()
	s.connections[id] = mockConnection{
		URN:      urnVal,
		ID:       id,
		Name:     name,
		Platform: platform,
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

// handleDeleteConnection handles deleteConnection GraphQL mutations.
func (s *mockServer) handleDeleteConnection(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	id := strings.TrimPrefix(urn, "urn:li:dataHubConnection:")

	s.mu.Lock()
	delete(s.connections, id)
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"deleteConnection": true},
	})
}

// handleConnectionItem serves GET /openapi/v3/entity/datahubconnection/{urn}.
// The blob is intentionally NOT included in the response to match the real
// server's behavior (where it is encrypted and unavailable to the caller).
func (s *mockServer) handleConnectionItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/datahubconnection/")
	id := strings.TrimPrefix(urn, "urn:li:dataHubConnection:")

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
}
