// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpsertCorpUser(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if r.Method != http.MethodPost {
				t.Errorf("method = %q, want POST", r.Method)
			}
			if r.URL.Path != "/openapi/v3/entity/corpuser" {
				t.Errorf("path = %q, want /openapi/v3/entity/corpuser", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("[]"))
		}))
		defer server.Close()

		c := newTestClient(t, server)
		urn, err := c.UpsertCorpUser(t.Context(), UpsertCorpUserInput{
			Username:    "alice",
			DisplayName: "Alice",
			Email:       "alice@example.com",
		})
		if err != nil {
			t.Fatalf("UpsertCorpUser() error = %v", err)
		}
		if urn != "urn:li:corpuser:alice" {
			t.Errorf("urn = %q, want urn:li:corpuser:alice", urn)
		}
	})

	t.Run("empty_username", func(t *testing.T) {
		c, _ := NewClient("http://localhost:8080", "test-token")
		_, err := c.UpsertCorpUser(t.Context(), UpsertCorpUserInput{Username: ""})
		if err == nil {
			t.Fatal("expected error for empty username")
		}
	})

	t.Run("http_403", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		c := newTestClient(t, server)
		_, err := c.UpsertCorpUser(t.Context(), UpsertCorpUserInput{Username: "alice"})
		if err == nil {
			t.Fatal("expected error for 403")
		}
	})
}

// corpUserInfoMockServer stores the corpUserInfo value from the last POST and
// serves it on GET. A nil stored value means the entity does not exist.
func corpUserInfoMockServer(t *testing.T, urn string, stored *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var payload []struct {
				Info struct {
					Value map[string]any `json:"value"`
				} `json:"corpUserInfo"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decoding POST payload: %v", err)
			}
			*stored = payload[0].Info.Value
			_, _ = w.Write([]byte("[]"))
		case http.MethodGet:
			if *stored == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"urn":          urn,
				"corpUserInfo": map[string]any{"value": *stored},
			})
		}
	}))
}

func TestUpsertCorpUserPreservesUnmanagedInfo(t *testing.T) {
	const urn = "urn:li:corpuser:alice"

	t.Run("preserves_unmanaged_fields", func(t *testing.T) {
		stored := map[string]any{
			"active":           false,
			"displayName":      "Alice",
			"fullName":         "Alice Example",
			"email":            "alice@example.com",
			"title":            "Engineer",
			"customProperties": map[string]any{"team": "a"},
			"managerUrn":       "urn:li:corpuser:bob",
			"departmentId":     float64(42),
			"departmentName":   "Data",
			"firstName":        "Alice",
			"lastName":         "Example",
			"countryCode":      "GB",
		}
		server := corpUserInfoMockServer(t, urn, &stored)
		defer server.Close()
		c := newTestClient(t, server)

		if _, err := c.UpsertCorpUser(t.Context(), UpsertCorpUserInput{Username: "alice", DisplayName: "Alice E."}); err != nil {
			t.Fatalf("UpsertCorpUser() error = %v", err)
		}

		if stored["displayName"] != "Alice E." {
			t.Errorf("displayName = %v, want Alice E.", stored["displayName"])
		}
		for _, k := range []string{"fullName", "email", "title"} {
			if _, ok := stored[k]; ok {
				t.Errorf("%s = %v, want it cleared", k, stored[k])
			}
		}
		if got := mustJSON(t, stored["customProperties"]); got != "{}" {
			t.Errorf("customProperties = %s, want {}", got)
		}
		want := map[string]any{
			"managerUrn":     "urn:li:corpuser:bob",
			"departmentId":   float64(42),
			"departmentName": "Data",
			"firstName":      "Alice",
			"lastName":       "Example",
			"countryCode":    "GB",
		}
		for k, v := range want {
			if stored[k] != v {
				t.Errorf("%s = %v, want %v preserved", k, stored[k], v)
			}
		}
		if stored["active"] != false {
			t.Errorf("active = %v, want false preserved", stored["active"])
		}
	})

	t.Run("active_on_create", func(t *testing.T) {
		var stored map[string]any
		server := corpUserInfoMockServer(t, urn, &stored)
		defer server.Close()
		c := newTestClient(t, server)

		if _, err := c.UpsertCorpUser(t.Context(), UpsertCorpUserInput{Username: "alice", Email: "alice@example.com"}); err != nil {
			t.Fatalf("UpsertCorpUser() error = %v", err)
		}
		if stored["active"] != true {
			t.Errorf("active = %v, want true on create", stored["active"])
		}
		if stored["email"] != "alice@example.com" {
			t.Errorf("email = %v, want alice@example.com", stored["email"])
		}
	})
}

func TestDeleteUser(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"removeUser": true},
			})
		}))
		defer server.Close()

		c := newTestClient(t, server)
		if err := c.DeleteUser(t.Context(), "urn:li:corpuser:alice"); err != nil {
			t.Fatalf("DeleteUser() error = %v", err)
		}
	})

	t.Run("empty_urn", func(t *testing.T) {
		c, _ := NewClient("http://localhost:8080", "test-token")
		if err := c.DeleteUser(t.Context(), ""); err == nil {
			t.Fatal("expected error for empty URN")
		}
	})
}

func TestListCorpUserURNs(t *testing.T) {
	t.Run("single_page", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"listUsers": map[string]any{
						"total": 2,
						"users": []map[string]any{
							{"urn": "urn:li:corpuser:alice"},
							{"urn": "urn:li:corpuser:bob"},
						},
					},
				},
			})
		}))
		defer server.Close()

		c := newTestClient(t, server)
		urns, err := c.ListCorpUserURNs(t.Context())
		if err != nil {
			t.Fatalf("ListCorpUserURNs() error = %v", err)
		}
		if len(urns) != 2 {
			t.Fatalf("got %d URNs, want 2", len(urns))
		}
	})

	t.Run("graphql_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]any{
					{"message": "forbidden"},
				},
			})
		}))
		defer server.Close()

		c := newTestClient(t, server)
		_, err := c.ListCorpUserURNs(t.Context())
		if err == nil {
			t.Fatal("expected error for GraphQL error response")
		}
	})
}
