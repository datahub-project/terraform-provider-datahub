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

// mockExecutorPool mirrors the RemoteExecutorPool GraphQL shape.
type mockExecutorPool struct {
	URN         string
	PoolID      string
	Description string
	IsDefault   bool
}

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
	pools            map[string]mockExecutorPool
	connections      map[string]mockConnection
	groups           map[string]mockGroup
	users            map[string]mockUser
	policies         map[string]mockPolicy
	defaultPoolID    string
	inviteToken      string
	resetTokens      map[string]string
	ossSignUpMode    bool
	// failDeleteFor holds source IDs whose next DELETE should return 500.
	// Entries are consumed on first use. Used by the /test-control endpoint.
	failDeleteFor map[string]struct{}
}

// NewServer starts an in-memory httptest.Server that mimics the DataHub API
// surface used by the provider. The server is closed automatically when t
// completes.
func NewServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := &mockServer{
		ingestionSources: make(map[string]mockIngestionSource),
		secrets:          make(map[string]mockSecret),
		pools:            make(map[string]mockExecutorPool),
		connections:      make(map[string]mockConnection),
		groups:           make(map[string]mockGroup),
		users:            make(map[string]mockUser),
		policies:         make(map[string]mockPolicy),
		inviteToken:      "mock-invite-token-001",
		resetTokens:      make(map[string]string),
		failDeleteFor:    make(map[string]struct{}),
	}
	s.seedUsers()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/graphql", s.handleGraphQL)
	mux.HandleFunc("/api/v2/graphql", s.handleGraphQL)
	mux.HandleFunc("/openapi/v3/entity/datahubingestionsource", s.handleIngestionSourceCollection)
	mux.HandleFunc("/openapi/v3/entity/datahubingestionsource/", s.handleIngestionSourceItem)
	mux.HandleFunc("/openapi/v3/entity/datahubsecret/", s.handleSecretItem)
	mux.HandleFunc("/openapi/v3/entity/datahubremoteexecutorpool/", s.handleExecutorPoolItem)
	mux.HandleFunc("/openapi/v3/entity/datahubconnection/", s.handleConnectionItem)
	mux.HandleFunc("/openapi/v3/entity/corpgroup/", s.handleCorpGroupItem)
	mux.HandleFunc("/auth/signUp", s.handleSignUp)
	mux.HandleFunc("/openapi/v3/entity/corpuser", s.handleCorpUserCollection)
	mux.HandleFunc("/openapi/v3/entity/corpuser/", s.handleCorpUserItem)
	mux.HandleFunc("/openapi/v3/entity/datahubrole/", s.handleDataHubRoleItem)
	mux.HandleFunc("/openapi/v3/entity/datahubpolicy/", s.handleDataHubPolicyItem)
	// Test-control endpoint: POST /test-control/force-delete-fail/{sourceID}
	// registers a one-shot 500 response for the next DELETE on that source.
	mux.HandleFunc("/test-control/force-delete-fail/", s.handleForceDeleteFail)
	mux.HandleFunc("/test-control/oss-signup-mode", s.handleOSSSignUpMode)
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
	case strings.Contains(q, "listIngestionSources"):
		s.handleListIngestionSources(w)
	case strings.Contains(q, "searchAcrossEntities"):
		s.handleSearchAcrossEntities(w, req.Variables)
	case strings.Contains(q, "upsertConnection"):
		s.handleCreateOrUpdateConnection(w, req.Variables)
	case strings.Contains(q, "createGroup"):
		s.handleCreateGroup(w, req.Variables)
	case strings.Contains(q, "updateName"):
		s.handleUpdateName(w, req.Variables)
	case strings.Contains(q, "updateCorpGroupProperties"):
		s.handleUpdateCorpGroupProperties(w, req.Variables)
	case strings.Contains(q, "addGroupMembers"):
		s.handleAddGroupMembers(w, req.Variables)
	case strings.Contains(q, "removeGroupMembers"):
		s.handleRemoveGroupMembers(w, req.Variables)
	case strings.Contains(q, "createNativeUserResetToken"):
		s.handleCreateNativeUserResetToken(w, req.Variables)
	case strings.Contains(q, "createInviteToken"):
		s.handleCreateInviteToken(w)
	case strings.Contains(q, "getInviteToken"):
		s.handleGetInviteToken(w)
	case strings.Contains(q, "removeUser"):
		s.handleRemoveUser(w, req.Variables)
	case strings.Contains(q, "listUsers"):
		s.handleListUsers(w)
	case strings.Contains(q, "removeGroup"):
		s.handleRemoveGroup(w, req.Variables)
	case strings.Contains(q, "listGroups"):
		s.handleListGroups(w)
	case strings.Contains(q, "batchAssignRole"):
		s.handleBatchAssignRole(w, req.Variables)
	case strings.Contains(q, "listRoles"):
		s.handleListRoles(w)
	case strings.Contains(q, "updatePolicy"):
		s.handleUpsertPolicy(w, req.Variables)
	case strings.Contains(q, "deletePolicy"):
		s.handleDeletePolicy(w, req.Variables)
	case strings.Contains(q, "listPolicies"):
		s.handleListPolicies(w)
	case strings.Contains(q, "createRemoteExecutorPool"):
		s.handleCreateExecutorPool(w, req.Variables)
	case strings.Contains(q, "updateDefaultRemoteExecutorPool"):
		s.handleUpdateDefaultExecutorPool(w, req.Variables)
	case strings.Contains(q, "updateRemoteExecutorPool"):
		s.handleUpdateExecutorPool(w, req.Variables)
	case strings.Contains(q, "getRemoteExecutorPool"):
		s.handleGetExecutorPool(w, req.Variables)
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
		// "*" means match all (DataHub wildcard). Otherwise mirror DataHub's
		// substring search; the client filters for exact match afterward.
		if query == "*" || strings.Contains(secret.Name, query) {
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
			"listSecrets": map[string]any{
				"total":   len(results),
				"secrets": results,
			},
		},
	})
}

func (s *mockServer) handleListIngestionSources(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var results []map[string]any
	for id, src := range s.ingestionSources {
		results = append(results, map[string]any{
			"urn": "urn:li:dataHubIngestionSource:" + id,
			// The list client only reads the URN; other fields can be omitted.
			"source": map[string]any{"type": src.DataHubIngestionSourceInfo.Value.Type},
		})
	}
	if results == nil {
		results = []map[string]any{}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"listIngestionSources": map[string]any{
				"total":            len(results),
				"ingestionSources": results,
			},
		},
	})
}

func (s *mockServer) handleSearchAcrossEntities(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	typesRaw, _ := input["types"].([]any)

	var entityTypes []string
	for _, t := range typesRaw {
		if ts, ok := t.(string); ok {
			entityTypes = append(entityTypes, ts)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var results []map[string]any

	for _, et := range entityTypes {
		switch et {
		case "DATAHUB_CONNECTION":
			for connID := range s.connections {
				results = append(results, map[string]any{
					"entity": map[string]any{
						"urn": "urn:li:dataHubConnection:" + connID,
					},
				})
			}
		}
	}
	if results == nil {
		results = []map[string]any{}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"searchAcrossEntities": map[string]any{
				"total":         len(results),
				"searchResults": results,
			},
		},
	})
}

// handleSecretItem serves GET /openapi/v3/entity/datahubsecret/{urn}.
func (s *mockServer) handleSecretItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/datahubsecret/")
	name := strings.TrimPrefix(urn, "urn:li:dataHubSecret:")

	s.mu.Lock()
	secret, ok := s.secrets[name]
	s.mu.Unlock()

	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"urn": secret.URN,
		"dataHubSecretKey": map[string]any{
			"value": map[string]any{"id": secret.Name},
		},
		"dataHubSecretValue": map[string]any{
			"value": map[string]any{
				"name":        secret.Name,
				"description": secret.Description,
			},
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

// poolGQL builds the GraphQL JSON shape for a mockExecutorPool.
func (s *mockServer) poolGQL(p mockExecutorPool) map[string]any {
	return map[string]any{
		"urn":            p.URN,
		"executorPoolId": p.PoolID,
		"description":    p.Description,
		"isDefault":      p.IsDefault,
		"isEmbedded":     false,
		"createdAt":      int64(0),
		"state": map[string]any{
			"status":  "READY",
			"message": "",
		},
	}
}

func (s *mockServer) handleCreateExecutorPool(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	poolID, _ := input["executorPoolId"].(string)
	desc, _ := input["description"].(string)
	isDefault, _ := input["isDefault"].(bool)

	urn := "urn:li:dataHubRemoteExecutorPool:" + poolID

	s.mu.Lock()
	s.pools[poolID] = mockExecutorPool{URN: urn, PoolID: poolID, Description: desc, IsDefault: isDefault}
	if isDefault {
		// demote previous default
		if s.defaultPoolID != "" && s.defaultPoolID != poolID {
			prev := s.pools[s.defaultPoolID]
			prev.IsDefault = false
			s.pools[s.defaultPoolID] = prev
		}
		s.defaultPoolID = poolID
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"createRemoteExecutorPool": urn},
	})
}

func (s *mockServer) handleUpdateExecutorPool(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	urn, _ := input["urn"].(string)
	poolID := strings.TrimPrefix(urn, "urn:li:dataHubRemoteExecutorPool:")
	desc, _ := input["description"].(string)

	s.mu.Lock()
	if p, ok := s.pools[poolID]; ok {
		p.Description = desc
		s.pools[poolID] = p
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"updateRemoteExecutorPool": true},
	})
}

func (s *mockServer) handleUpdateDefaultExecutorPool(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	poolID := strings.TrimPrefix(urn, "urn:li:dataHubRemoteExecutorPool:")

	s.mu.Lock()
	if s.defaultPoolID != "" && s.defaultPoolID != poolID {
		prev := s.pools[s.defaultPoolID]
		prev.IsDefault = false
		s.pools[s.defaultPoolID] = prev
	}
	if p, ok := s.pools[poolID]; ok {
		p.IsDefault = true
		s.pools[poolID] = p
	}
	s.defaultPoolID = poolID
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"updateDefaultRemoteExecutorPool": true},
	})
}

func (s *mockServer) handleGetExecutorPool(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	poolID := strings.TrimPrefix(urn, "urn:li:dataHubRemoteExecutorPool:")

	s.mu.Lock()
	p, ok := s.pools[poolID]
	s.mu.Unlock()

	if !ok {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"getRemoteExecutorPool": nil},
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"getRemoteExecutorPool": s.poolGQL(p)},
	})
}

// handleExecutorPoolItem handles DELETE /openapi/v3/entity/datahubremoteexecutorpool/{urn}.
func (s *mockServer) handleExecutorPoolItem(w http.ResponseWriter, r *http.Request) {
	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/datahubremoteexecutorpool/")
	poolID := strings.TrimPrefix(urn, "urn:li:dataHubRemoteExecutorPool:")

	switch r.Method {
	case http.MethodDelete:
		s.mu.Lock()
		delete(s.pools, poolID)
		if s.defaultPoolID == poolID {
			s.defaultPoolID = ""
		}
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	default:
		http.NotFound(w, r)
	}
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
		_, shouldFail := s.failDeleteFor[sourceID]
		if shouldFail {
			delete(s.failDeleteFor, sourceID)
			s.mu.Unlock()
			http.Error(w, "forced delete failure", http.StatusInternalServerError)
			return
		}
		delete(s.ingestionSources, sourceID)
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)

	default:
		http.NotFound(w, r)
	}
}

// handleForceDeleteFail registers a one-shot 500 response for the next DELETE
// on the given source. Called from test PreConfig functions via:
//
//	POST /test-control/force-delete-fail/{sourceID}
func (s *mockServer) handleForceDeleteFail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sourceID := strings.TrimPrefix(r.URL.Path, "/test-control/force-delete-fail/")
	if sourceID == "" {
		http.Error(w, "missing sourceID", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.failDeleteFor[sourceID] = struct{}{}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// handleOSSSignUpMode toggles the mock's signUp guard between OSS behavior
// (reject any pre-existing entity) and Cloud behavior (reject only if
// credentials already exist). Default is Cloud. Called from test PreConfig:
//
//	POST /test-control/oss-signup-mode   (enables OSS mode)
//	DELETE /test-control/oss-signup-mode  (reverts to Cloud mode)
func (s *mockServer) handleOSSSignUpMode(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	switch r.Method {
	case http.MethodPost:
		s.ossSignUpMode = true
	case http.MethodDelete:
		s.ossSignUpMode = false
	default:
		s.mu.Unlock()
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
