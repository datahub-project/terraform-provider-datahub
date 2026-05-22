// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// Package datahubtesting provides an in-memory mock DataHub server and
// target-agnostic scenario helpers for Terraform acceptance tests.
//
// Tests using this package point the provider at the mock server via t.Setenv:
//
//	server := datahubtesting.NewServer(t)
//	t.Setenv("DATAHUB_GMS_URL", server.URL)
//	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")
//
// The same scenario functions can be re-used against a live DataHub instance
// (see the _live_test.go pattern) without modification.
package datahubtesting

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// mockIngestionSource mirrors the JSON shape that pkg/datahub sends and reads.
type mockIngestionSource struct {
	Urn                       string `json:"urn"`
	DataHubIngestionSourceKey struct {
		Value struct {
			ID string `json:"id"`
		} `json:"value"`
	} `json:"dataHubIngestionSourceKey"`
	DataHubIngestionSourceInfo struct {
		Value struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Platform string `json:"platform,omitempty"`
			Schedule *struct {
				Interval string `json:"interval"`
				Timezone string `json:"timezone"`
			} `json:"schedule,omitempty"`
			Config struct {
				Recipe     string            `json:"recipe"`
				Version    string            `json:"version,omitempty"`
				ExecutorID string            `json:"executorId,omitempty"`
				ExtraArgs  map[string]string `json:"extraArgs,omitempty"`
				DebugMode  *bool             `json:"debugMode,omitempty"`
			} `json:"config"`
		} `json:"value"`
	} `json:"dataHubIngestionSourceInfo"`
}

type mockSecret struct {
	URN         string
	Name        string
	Description string
}

type mockServer struct {
	mu               sync.Mutex
	ingestionSources map[string]mockIngestionSource
	secrets          map[string]mockSecret
}

// NewServer starts an in-memory httptest.Server that mimics the DataHub API
// surface used by the provider. The server is closed automatically when t
// completes.
func NewServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := &mockServer{
		ingestionSources: make(map[string]mockIngestionSource),
		secrets:          make(map[string]mockSecret),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/graphql", s.handleGraphQL)
	mux.HandleFunc("/openapi/v3/entity/datahubingestionsource", s.handleIngestionSourceCollection)
	mux.HandleFunc("/openapi/v3/entity/datahubingestionsource/", s.handleIngestionSourceItem)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// handleGraphQL dispatches GraphQL operations to the appropriate mock handler.
func (s *mockServer) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	body, _ := io.ReadAll(r.Body)
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, `{"errors":[{"message":"bad request"}]}`, http.StatusBadRequest)
		return
	}

	q := req.Query
	switch {
	case strings.Contains(q, "me {"):
		s.handleMe(w)
	case strings.Contains(q, "createSecret"):
		s.handleCreateSecret(w, req.Variables)
	case strings.Contains(q, "updateSecret"):
		s.handleUpdateSecret(w, req.Variables)
	case strings.Contains(q, "deleteSecret"):
		s.handleDeleteSecret(w, req.Variables)
	case strings.Contains(q, "listSecrets"):
		s.handleListSecrets(w, req.Variables)
	default:
		http.Error(w, `{"errors":[{"message":"unknown operation"}]}`, http.StatusBadRequest)
	}
}

func (s *mockServer) handleMe(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"me": map[string]any{
				"corpUser": map[string]any{
					"urn":      "urn:li:corpuser:testuser",
					"username": "testuser",
					"type":     "CORP_USER",
					"info": map[string]any{
						"displayName": "Test User",
						"email":       "testuser@example.com",
					},
				},
			},
		},
	})
}

func (s *mockServer) handleCreateSecret(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	name, _ := input["name"].(string)
	desc, _ := input["description"].(string)

	s.mu.Lock()
	s.secrets[name] = mockSecret{
		URN:         "urn:li:dataHubSecret:" + name,
		Name:        name,
		Description: desc,
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"createSecret": "urn:li:dataHubSecret:" + name},
	})
}

func (s *mockServer) handleUpdateSecret(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	urn, _ := input["urn"].(string)
	name, _ := input["name"].(string)
	desc, _ := input["description"].(string)

	s.mu.Lock()
	s.secrets[name] = mockSecret{URN: urn, Name: name, Description: desc}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"updateSecret": urn},
	})
}

func (s *mockServer) handleDeleteSecret(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	name := strings.TrimPrefix(urn, "urn:li:dataHubSecret:")

	s.mu.Lock()
	delete(s.secrets, name)
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"deleteSecret": urn},
	})
}

func (s *mockServer) handleListSecrets(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	query, _ := input["query"].(string)

	s.mu.Lock()
	defer s.mu.Unlock()

	var results []map[string]any
	for _, secret := range s.secrets {
		// Mirror DataHub's substring search: include if name contains query.
		// The client filters for exact match afterward.
		if strings.Contains(secret.Name, query) {
			results = append(results, map[string]any{
				"urn":         secret.URN,
				"name":        secret.Name,
				"description": secret.Description,
			})
		}
	}
	if results == nil {
		results = []map[string]any{}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"listSecrets": map[string]any{"secrets": results},
		},
	})
}

// handleIngestionSourceCollection handles POST to the collection endpoint.
func (s *mockServer) handleIngestionSourceCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

	body, _ := io.ReadAll(r.Body)
	var entities []mockIngestionSource
	if err := json.Unmarshal(body, &entities); err != nil || len(entities) == 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	entity := entities[0]
	sourceID := strings.TrimPrefix(entity.Urn, "urn:li:dataHubIngestionSource:")

	s.mu.Lock()
	s.ingestionSources[sourceID] = entity
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// handleIngestionSourceItem handles GET and DELETE on a single entity by URN.
func (s *mockServer) handleIngestionSourceItem(w http.ResponseWriter, r *http.Request) {
	// Path: /openapi/v3/entity/datahubingestionsource/{urn}
	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/datahubingestionsource/")
	sourceID := strings.TrimPrefix(urn, "urn:li:dataHubIngestionSource:")

	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		entity, ok := s.ingestionSources[sourceID]
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(entity)

	case http.MethodDelete:
		s.mu.Lock()
		delete(s.ingestionSources, sourceID)
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)

	default:
		http.NotFound(w, r)
	}
}
