// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient returns a Client pointed at the given httptest server.
func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	c, err := NewClient(server.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestMe(t *testing.T) {
	t.Run("success_full_identity", func(t *testing.T) {
		var capturedMethod, capturedPath, capturedAuth, capturedBody string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedMethod = r.Method
			capturedPath = r.URL.Path
			capturedAuth = r.Header.Get("Authorization")
			b, _ := io.ReadAll(r.Body)
			capturedBody = string(b)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"me": map[string]any{
						"corpUser": map[string]any{
							"urn":      "urn:li:corpuser:jane",
							"username": "jane",
							"type":     "CORP_USER",
							"info": map[string]any{
								"displayName": "Jane Smith",
								"email":       "jane@example.com",
							},
						},
					},
				},
			})
		}))
		defer server.Close()

		c := newTestClient(t, server)
		id, err := c.Me(t.Context())
		if err != nil {
			t.Fatalf("Me() error = %v", err)
		}

		if capturedMethod != http.MethodPost {
			t.Errorf("method = %q, want POST", capturedMethod)
		}
		if capturedPath != "/api/graphql" {
			t.Errorf("path = %q, want /api/graphql", capturedPath)
		}
		if !strings.HasPrefix(capturedAuth, "Bearer ") {
			t.Errorf("Authorization = %q, want Bearer ...", capturedAuth)
		}
		if !strings.Contains(capturedBody, "me { corpUser") {
			t.Errorf("request body missing expected query fragment, got: %s", capturedBody)
		}
		if id.Urn != "urn:li:corpuser:jane" {
			t.Errorf("Urn = %q, want urn:li:corpuser:jane", id.Urn)
		}
		if id.Username != "jane" {
			t.Errorf("Username = %q, want jane", id.Username)
		}
		if id.Type != "CORP_USER" {
			t.Errorf("Type = %q, want CORP_USER", id.Type)
		}
		if id.DisplayName != "Jane Smith" {
			t.Errorf("DisplayName = %q, want Jane Smith", id.DisplayName)
		}
		if id.Email != "jane@example.com" {
			t.Errorf("Email = %q, want jane@example.com", id.Email)
		}
	})

	t.Run("null_info_subtree", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"me": map[string]any{
						"corpUser": map[string]any{
							"urn":      "urn:li:corpuser:svc",
							"username": "svc",
							"type":     "CORP_USER",
							"info":     nil,
						},
					},
				},
			})
		}))
		defer server.Close()

		c := newTestClient(t, server)
		id, err := c.Me(t.Context())
		if err != nil {
			t.Fatalf("Me() error = %v", err)
		}
		if id.DisplayName != "" {
			t.Errorf("DisplayName = %q, want empty", id.DisplayName)
		}
		if id.Email != "" {
			t.Errorf("Email = %q, want empty", id.Email)
		}
	})

	t.Run("http_401", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		c := newTestClient(t, server)
		_, err := c.Me(t.Context())
		if err == nil {
			t.Fatal("Me() expected error for 401, got nil")
		}
		if !strings.Contains(err.Error(), "rejected") && !strings.Contains(err.Error(), "401") {
			t.Errorf("error = %q, expected mention of rejected/401", err.Error())
		}
	})

	t.Run("graphql_errors_on_200", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]any{
					{"message": "Unauthorized: token expired"},
				},
			})
		}))
		defer server.Close()

		c := newTestClient(t, server)
		_, err := c.Me(t.Context())
		if err == nil {
			t.Fatal("Me() expected error for GraphQL errors, got nil")
		}
		if !strings.Contains(err.Error(), "Unauthorized: token expired") {
			t.Errorf("error = %q, expected GraphQL error message", err.Error())
		}
	})

	t.Run("malformed_json", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("this is not json {{{"))
		}))
		defer server.Close()

		c := newTestClient(t, server)
		_, err := c.Me(t.Context())
		if err == nil {
			t.Fatal("Me() expected error for malformed JSON, got nil")
		}
		if !strings.Contains(err.Error(), "parsing") {
			t.Errorf("error = %q, expected mention of parsing", err.Error())
		}
	})
}
