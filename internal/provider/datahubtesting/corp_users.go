// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// mockUser mirrors the corpUser shape the provider reads. NativeGroups reflects
// the nativeGroupMembership aspect (maintained by group-member mutations).
// RoleURN reflects the single roleMembership entry (maintained by role
// assignment mutations).
type mockUser struct {
	URN            string
	Username       string
	FullName       string
	DisplayName    string
	Email          string
	Title          string
	Active         bool
	Status         string
	HasCredentials bool
	NativeGroups   []string
	RoleURN        string
	SubTypes       []string
}

// seedUsers pre-populates a couple of users so corp_user lookups, group
// membership, and role assignment scenarios have real actors to reference.
// "testuser" matches the identity returned by handleMe.
func (s *mockServer) seedUsers() {
	s.users["testuser"] = mockUser{
		URN:         "urn:li:corpuser:testuser",
		Username:    "testuser",
		DisplayName: "Test User",
		Email:       "testuser@example.com",
		Title:       "Engineer",
		Active:      true,
		Status:      "ACTIVE",
	}
	s.users["datahub"] = mockUser{
		URN:         "urn:li:corpuser:datahub",
		Username:    "datahub",
		DisplayName: "DataHub",
		Active:      true,
	}
	// A seeded service account (corpUser + SERVICE_ACCOUNT subtype) for
	// service-account data-source and import scenarios.
	s.users["service_seed"] = mockUser{
		URN:         "urn:li:corpuser:service_seed",
		Username:    "service_seed",
		DisplayName: "Seed Service Account",
		Title:       "Seeded for tests",
		Active:      true,
		SubTypes:    []string{"SERVICE_ACCOUNT"},
	}
	// A service_-prefixed corpUser WITHOUT the subtype, to exercise the
	// service-account resource's subtype guard (it must refuse to manage this).
	s.users["service_faker"] = mockUser{
		URN:         "urn:li:corpuser:service_faker",
		Username:    "service_faker",
		DisplayName: "Not Actually A Service Account",
		Active:      true,
	}
}

// handleCorpUserItem serves GET /openapi/v3/entity/corpuser/{urn}, returning the
// same aspect shape as the real OpenAPI v3 endpoint.
func (s *mockServer) handleCorpUserItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/corpuser/")
	username := strings.TrimPrefix(urn, "urn:li:corpuser:")

	s.mu.Lock()
	u, ok := s.users[username]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	entity := map[string]any{
		"urn": u.URN,
		"corpUserKey": map[string]any{
			"value": map[string]any{"username": u.Username},
		},
		"corpUserInfo": map[string]any{
			"value": map[string]any{
				"fullName":    u.FullName,
				"displayName": u.DisplayName,
				"email":       u.Email,
				"title":       u.Title,
				"active":      u.Active,
			},
		},
	}
	if u.Status != "" {
		entity["corpUserStatus"] = map[string]any{
			"value": map[string]any{"status": u.Status},
		}
	}
	if len(u.NativeGroups) > 0 {
		entity["nativeGroupMembership"] = map[string]any{
			"value": map[string]any{"nativeGroups": u.NativeGroups},
		}
	}
	if u.RoleURN != "" {
		entity["roleMembership"] = map[string]any{
			"value": map[string]any{"roles": []string{u.RoleURN}},
		}
	}
	if len(u.SubTypes) > 0 {
		entity["subTypes"] = map[string]any{
			"value": map[string]any{"typeNames": u.SubTypes},
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entity)
}

// handleCorpUserCollection serves POST /openapi/v3/entity/corpuser for upsert.
func (s *mockServer) handleCorpUserCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var entities []struct {
		URN     string `json:"urn"`
		KeyData *struct {
			Value struct {
				Username string `json:"username"`
			} `json:"value"`
		} `json:"corpUserKey"`
		Info *struct {
			Value struct {
				FullName    string `json:"fullName"`
				DisplayName string `json:"displayName"`
				Email       string `json:"email"`
				Title       string `json:"title"`
				Active      bool   `json:"active"`
			} `json:"value"`
		} `json:"corpUserInfo"`
		SubTypes *struct {
			Value struct {
				TypeNames []string `json:"typeNames"`
			} `json:"value"`
		} `json:"subTypes"`
	}
	if err := json.Unmarshal(body, &entities); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, e := range entities {
		username := ""
		if e.KeyData != nil {
			username = e.KeyData.Value.Username
		}
		if username == "" {
			username = strings.TrimPrefix(e.URN, "urn:li:corpuser:")
		}

		existing, exists := s.users[username]
		u := mockUser{
			URN:      e.URN,
			Username: username,
			Active:   true,
		}
		if exists {
			u.NativeGroups = existing.NativeGroups
			u.RoleURN = existing.RoleURN
			u.Status = existing.Status
			u.SubTypes = existing.SubTypes
		}
		if e.Info != nil {
			u.FullName = e.Info.Value.FullName
			u.DisplayName = e.Info.Value.DisplayName
			u.Email = e.Info.Value.Email
			u.Title = e.Info.Value.Title
			u.Active = e.Info.Value.Active
		}
		if e.SubTypes != nil {
			u.SubTypes = e.SubTypes.Value.TypeNames
		}
		s.users[username] = u
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("[]"))
}

// handleRemoveUser handles the removeUser GraphQL mutation.
func (s *mockServer) handleRemoveUser(w http.ResponseWriter, vars map[string]any) {
	urn, _ := vars["urn"].(string)
	username := strings.TrimPrefix(urn, "urn:li:corpuser:")

	s.mu.Lock()
	delete(s.users, username)
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"removeUser": true},
	})
}

// handleListUsers handles the listUsers GraphQL query.
func (s *mockServer) handleListUsers(w http.ResponseWriter) {
	s.mu.Lock()
	users := make([]map[string]any, 0, len(s.users))
	for _, u := range s.users {
		users = append(users, map[string]any{"urn": u.URN})
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"listUsers": map[string]any{
				"total": len(users),
				"users": users,
			},
		},
	})
}

// handleListServiceAccounts handles the listServiceAccounts GraphQL query,
// returning only corpUsers carrying the SERVICE_ACCOUNT subtype.
func (s *mockServer) handleListServiceAccounts(w http.ResponseWriter) {
	s.mu.Lock()
	accounts := make([]map[string]any, 0)
	for _, u := range s.users {
		isSA := false
		for _, t := range u.SubTypes {
			if t == "SERVICE_ACCOUNT" {
				isSA = true
				break
			}
		}
		if isSA {
			accounts = append(accounts, map[string]any{"urn": u.URN})
		}
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"listServiceAccounts": map[string]any{
				"total":           len(accounts),
				"serviceAccounts": accounts,
			},
		},
	})
}

// handleSignUp handles POST /signUp (native user creation).
func (s *mockServer) handleSignUp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var payload struct {
		UserURN     string `json:"userUrn"`
		FullName    string `json:"fullName"`
		Email       string `json:"email"`
		Password    string `json:"password"`
		Title       string `json:"title"`
		InviteToken string `json:"inviteToken"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return
	}

	username := strings.TrimPrefix(payload.UserURN, "urn:li:corpuser:")

	s.mu.Lock()
	defer s.mu.Unlock()

	if payload.InviteToken != s.inviteToken {
		http.Error(w, "Invalid invite token", http.StatusBadRequest)
		return
	}

	existing, exists := s.users[username]
	if exists {
		if s.ossSignUpMode || existing.HasCredentials {
			http.Error(w, "This user already exists! Cannot create a new user.", http.StatusBadRequest)
			return
		}
	}

	u := mockUser{
		URN:            payload.UserURN,
		Username:       username,
		FullName:       payload.FullName,
		DisplayName:    payload.FullName,
		Email:          payload.Email,
		Title:          payload.Title,
		Active:         true,
		Status:         "ACTIVE",
		HasCredentials: true,
	}
	if exists {
		u.NativeGroups = existing.NativeGroups
		u.RoleURN = existing.RoleURN
	}
	s.users[username] = u

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"message":"User created"}`))
}

// handleGetInviteToken handles the getInviteToken GraphQL query.
func (s *mockServer) handleGetInviteToken(w http.ResponseWriter) {
	s.mu.Lock()
	token := s.inviteToken
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"getInviteToken": map[string]any{
				"inviteToken": token,
			},
		},
	})
}

// handleCreateInviteToken handles the createInviteToken GraphQL mutation.
func (s *mockServer) handleCreateInviteToken(w http.ResponseWriter) {
	s.mu.Lock()
	s.inviteToken = "mock-invite-token-regenerated"
	token := s.inviteToken
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"createInviteToken": map[string]any{
				"inviteToken": token,
			},
		},
	})
}

// handleCreateNativeUserResetToken handles the createNativeUserResetToken mutation.
func (s *mockServer) handleCreateNativeUserResetToken(w http.ResponseWriter, vars map[string]any) {
	input, _ := vars["input"].(map[string]any)
	userURN, _ := input["userUrn"].(string)

	token := "mock-reset-token-for-" + strings.TrimPrefix(userURN, "urn:li:corpuser:")

	s.mu.Lock()
	s.resetTokens[userURN] = token
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"createNativeUserResetToken": map[string]any{
				"resetToken": token,
			},
		},
	})
}
