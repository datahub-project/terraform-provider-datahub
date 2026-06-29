// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"net/http"
	"strings"
)

const mockActionPipelineURNPrefix = "urn:li:dataHubAction:"

// mockActionPipeline stores the in-memory state for one action pipeline.
type mockActionPipeline struct {
	ID          string
	Name        string
	Type        string
	Category    string
	Description string
	Recipe      string // stored verbatim (recipe round-trips byte-for-byte)
	ExecutorID  string
	Version     string
	DebugMode   *bool
}

// handleUpsertActionPipeline handles the upsertActionPipeline GraphQL mutation.
// The mutation returns the URN as a bare String.
func (s *mockServer) handleUpsertActionPipeline(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	input, _ := variables["input"].(map[string]any)
	id := strings.TrimPrefix(urn, mockActionPipelineURNPrefix)

	ap := mockActionPipeline{ID: id}
	ap.Name, _ = input["name"].(string)
	ap.Type, _ = input["type"].(string)
	ap.Category, _ = input["category"].(string)
	ap.Description, _ = input["description"].(string)
	if cfg, ok := input["config"].(map[string]any); ok {
		ap.Recipe, _ = cfg["recipe"].(string)
		ap.ExecutorID, _ = cfg["executorId"].(string)
		ap.Version, _ = cfg["version"].(string)
		if dm, ok := cfg["debugMode"].(bool); ok {
			ap.DebugMode = &dm
		}
	}

	s.mu.Lock()
	s.actionPipelines[id] = ap
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"upsertActionPipeline": urn},
	})
}

// handleDeleteActionPipeline handles the deleteActionPipeline GraphQL mutation.
func (s *mockServer) handleDeleteActionPipeline(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	id := strings.TrimPrefix(urn, mockActionPipelineURNPrefix)

	s.mu.Lock()
	delete(s.actionPipelines, id)
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"deleteActionPipeline": "true"},
	})
}

// handleListActionPipelines handles the listActionPipelines GraphQL query.
func (s *mockServer) handleListActionPipelines(w http.ResponseWriter, _ map[string]any) {
	s.mu.Lock()
	pipelines := make([]map[string]any, 0, len(s.actionPipelines))
	for id := range s.actionPipelines {
		pipelines = append(pipelines, map[string]any{"urn": mockActionPipelineURNPrefix + id})
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"listActionPipelines": map[string]any{
				"total":           len(pipelines),
				"actionPipelines": pipelines,
			},
		},
	})
}

// handleActionPipelineItem handles GET /openapi/v3/entity/datahubaction/{urn}.
func (s *mockServer) handleActionPipelineItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/datahubaction/")
	id := strings.TrimPrefix(urn, mockActionPipelineURNPrefix)

	s.mu.Lock()
	ap, ok := s.actionPipelines[id]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	config := map[string]any{"recipe": ap.Recipe}
	if ap.ExecutorID != "" {
		config["executorId"] = ap.ExecutorID
	}
	if ap.Version != "" {
		config["version"] = ap.Version
	}
	if ap.DebugMode != nil {
		config["debugMode"] = *ap.DebugMode
	}
	infoValue := map[string]any{
		"name":   ap.Name,
		"type":   ap.Type,
		"config": config,
	}
	if ap.Category != "" {
		infoValue["category"] = ap.Category
	}
	if ap.Description != "" {
		infoValue["description"] = ap.Description
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"urn":              mockActionPipelineURNPrefix + id,
		"dataHubActionKey": map[string]any{"value": map[string]any{"id": id}},
		"dataHubActionInfo": map[string]any{
			"value": infoValue,
		},
	})
}
