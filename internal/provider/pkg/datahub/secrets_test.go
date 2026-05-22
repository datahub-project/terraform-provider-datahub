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

// graphqlHandler is a test double for the DataHub GraphQL endpoint. It inspects
// the incoming query string and returns scripted responses.
type graphqlHandler struct {
	createResp func(name, value, desc string) any
	listResp   func(query string) any
	updateResp func() any
	deleteResp func(urn string) any
}

func (h *graphqlHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	_ = json.Unmarshal(body, &req)

	switch {
	case strings.Contains(req.Query, "createSecret"):
		input, ok := req.Variables["input"].(map[string]any)
		if !ok {
			http.Error(w, `{"errors":[{"message":"bad input"}]}`, http.StatusBadRequest)
			return
		}
		name, _ := input["name"].(string)
		value, _ := input["value"].(string)
		desc, _ := input["description"].(string)
		_ = json.NewEncoder(w).Encode(h.createResp(name, value, desc))
	case strings.Contains(req.Query, "updateSecret"):
		_ = json.NewEncoder(w).Encode(h.updateResp())
	case strings.Contains(req.Query, "deleteSecret"):
		urn, _ := req.Variables["urn"].(string)
		_ = json.NewEncoder(w).Encode(h.deleteResp(urn))
	case strings.Contains(req.Query, "listSecrets"):
		input, ok := req.Variables["input"].(map[string]any)
		if !ok {
			http.Error(w, `{"errors":[{"message":"bad input"}]}`, http.StatusBadRequest)
			return
		}
		query, _ := input["query"].(string)
		_ = json.NewEncoder(w).Encode(h.listResp(query))
	default:
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":[{"message":"unknown operation"}]}`))
	}
}

func TestCreateSecret(t *testing.T) {
	t.Run("success_returns_urn", func(t *testing.T) {
		handler := &graphqlHandler{
			createResp: func(name, _, _ string) any {
				return map[string]any{"data": map[string]any{"createSecret": "urn:li:dataHubSecret:" + name}}
			},
		}
		server := httptest.NewServer(handler)
		defer server.Close()

		c := newTestClient(t, server)
		urn, err := c.CreateSecret(t.Context(), CreateSecretInput{Name: "my-secret", Value: "s3cr3t", Description: "test"})
		if err != nil {
			t.Fatalf("CreateSecret() error = %v", err)
		}
		if urn != "urn:li:dataHubSecret:my-secret" {
			t.Errorf("URN = %q, want urn:li:dataHubSecret:my-secret", urn)
		}
	})

	t.Run("empty_name_returns_error", func(t *testing.T) {
		c := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})))
		_, err := c.CreateSecret(t.Context(), CreateSecretInput{Name: "", Value: "x"})
		if err == nil {
			t.Fatal("expected error for empty name, got nil")
		}
	})

	t.Run("empty_value_returns_error", func(t *testing.T) {
		c := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})))
		_, err := c.CreateSecret(t.Context(), CreateSecretInput{Name: "x", Value: ""})
		if err == nil {
			t.Fatal("expected error for empty value, got nil")
		}
	})

	t.Run("duplicate_secret_returns_helpful_error", func(t *testing.T) {
		handler := &graphqlHandler{
			createResp: func(_, _, _ string) any {
				return map[string]any{"errors": []map[string]any{{"message": "This Secret already exists"}}}
			},
		}
		server := httptest.NewServer(handler)
		defer server.Close()

		c := newTestClient(t, server)
		_, err := c.CreateSecret(t.Context(), CreateSecretInput{Name: "dup", Value: "x"})
		if err == nil {
			t.Fatal("expected error for duplicate secret, got nil")
		}
		if !strings.Contains(err.Error(), "import") {
			t.Errorf("error = %q, expected import hint", err.Error())
		}
	})

	t.Run("graphql_error_returns_error", func(t *testing.T) {
		handler := &graphqlHandler{
			createResp: func(_, _, _ string) any {
				return map[string]any{"errors": []map[string]any{{"message": "permission denied"}}}
			},
		}
		server := httptest.NewServer(handler)
		defer server.Close()

		c := newTestClient(t, server)
		_, err := c.CreateSecret(t.Context(), CreateSecretInput{Name: "x", Value: "y"})
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("error = %v, want mention of permission denied", err)
		}
	})

	t.Run("http_401_returns_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()
		c := newTestClient(t, server)
		_, err := c.CreateSecret(t.Context(), CreateSecretInput{Name: "x", Value: "y"})
		if err == nil {
			t.Fatal("expected error for 401, got nil")
		}
	})
}

func TestGetSecretByName(t *testing.T) {
	t.Run("found_returns_secret", func(t *testing.T) {
		handler := &graphqlHandler{
			listResp: func(_ string) any {
				return map[string]any{
					"data": map[string]any{
						"listSecrets": map[string]any{
							"secrets": []map[string]any{
								{"urn": "urn:li:dataHubSecret:my-secret", "name": "my-secret", "description": "a desc"},
							},
						},
					},
				}
			},
		}
		server := httptest.NewServer(handler)
		defer server.Close()

		c := newTestClient(t, server)
		secret, err := c.GetSecretByName(t.Context(), "my-secret")
		if err != nil {
			t.Fatalf("GetSecretByName() error = %v", err)
		}
		if secret == nil {
			t.Fatal("secret is nil, want non-nil")
		}
		if secret.Name != "my-secret" {
			t.Errorf("Name = %q, want my-secret", secret.Name)
		}
		if secret.Description != "a desc" {
			t.Errorf("Description = %q, want 'a desc'", secret.Description)
		}
	})

	t.Run("not_found_returns_nil", func(t *testing.T) {
		handler := &graphqlHandler{
			listResp: func(_ string) any {
				return map[string]any{
					"data": map[string]any{
						"listSecrets": map[string]any{"secrets": []any{}},
					},
				}
			},
		}
		server := httptest.NewServer(handler)
		defer server.Close()

		c := newTestClient(t, server)
		secret, err := c.GetSecretByName(t.Context(), "missing")
		if err != nil {
			t.Fatalf("unexpected error = %v", err)
		}
		if secret != nil {
			t.Errorf("expected nil for missing secret, got %+v", secret)
		}
	})

	// listSecrets uses substring search; the client must filter for exact name match.
	t.Run("filters_exact_name_from_substring_results", func(t *testing.T) {
		handler := &graphqlHandler{
			listResp: func(_ string) any {
				return map[string]any{
					"data": map[string]any{
						"listSecrets": map[string]any{
							"secrets": []map[string]any{
								{"urn": "urn:li:dataHubSecret:my-secret", "name": "my-secret", "description": ""},
								{"urn": "urn:li:dataHubSecret:my-secret-v2", "name": "my-secret-v2", "description": ""},
							},
						},
					},
				}
			},
		}
		server := httptest.NewServer(handler)
		defer server.Close()

		c := newTestClient(t, server)
		secret, err := c.GetSecretByName(t.Context(), "my-secret")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if secret == nil || secret.Name != "my-secret" {
			t.Errorf("expected exact match my-secret, got %+v", secret)
		}
	})

	t.Run("empty_name_returns_error", func(t *testing.T) {
		c := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})))
		_, err := c.GetSecretByName(t.Context(), "")
		if err == nil {
			t.Fatal("expected error for empty name, got nil")
		}
	})
}

func TestGetSecretByURN(t *testing.T) {
	t.Run("found_returns_secret", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"urn": "urn:li:dataHubSecret:my-secret",
				"dataHubSecretKey": map[string]any{
					"value": map[string]any{"id": "my-secret"},
				},
				"dataHubSecretValue": map[string]any{
					"value": map[string]any{
						"name":        "my-secret",
						"description": "a desc",
					},
				},
			})
		}))
		defer server.Close()

		c := newTestClient(t, server)
		secret, err := c.GetSecretByURN(t.Context(), "urn:li:dataHubSecret:my-secret")
		if err != nil {
			t.Fatalf("GetSecretByURN() error = %v", err)
		}
		if secret == nil {
			t.Fatal("secret is nil, want non-nil")
		}
		if secret.Name != "my-secret" {
			t.Errorf("Name = %q, want my-secret", secret.Name)
		}
		if secret.Description != "a desc" {
			t.Errorf("Description = %q, want 'a desc'", secret.Description)
		}
	})

	t.Run("not_found_returns_nil", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.NotFound(w, nil)
		}))
		defer server.Close()

		c := newTestClient(t, server)
		secret, err := c.GetSecretByURN(t.Context(), "urn:li:dataHubSecret:missing")
		if err != nil {
			t.Fatalf("unexpected error = %v", err)
		}
		if secret != nil {
			t.Errorf("expected nil for missing secret, got %+v", secret)
		}
	})

	t.Run("empty_urn_returns_error", func(t *testing.T) {
		c := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})))
		_, err := c.GetSecretByURN(t.Context(), "")
		if err == nil {
			t.Fatal("expected error for empty URN, got nil")
		}
	})
}

func TestUpdateSecret(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var gotBody []byte
		handler := &graphqlHandler{
			updateResp: func() any {
				return map[string]any{"data": map[string]any{"updateSecret": "urn:li:dataHubSecret:my-secret"}}
			},
		}
		// Wrap to capture raw body.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotBody, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(strings.NewReader(string(gotBody)))
			handler.ServeHTTP(w, r)
		}))
		defer server.Close()

		c := newTestClient(t, server)
		err := c.UpdateSecret(t.Context(), UpdateSecretInput{
			URN: "urn:li:dataHubSecret:my-secret", Name: "my-secret", Value: "newval", Description: "new desc",
		})
		if err != nil {
			t.Fatalf("UpdateSecret() error = %v", err)
		}
		if !strings.Contains(string(gotBody), "updateSecret") {
			t.Errorf("body = %q, want updateSecret mutation", gotBody)
		}
	})

	t.Run("missing_urn_returns_error", func(t *testing.T) {
		c := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})))
		err := c.UpdateSecret(t.Context(), UpdateSecretInput{Name: "x", Value: "y"})
		if err == nil {
			t.Fatal("expected error for empty URN, got nil")
		}
	})
}

func TestDeleteSecret(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		handler := &graphqlHandler{
			deleteResp: func(urn string) any {
				return map[string]any{"data": map[string]any{"deleteSecret": urn}}
			},
		}
		server := httptest.NewServer(handler)
		defer server.Close()

		c := newTestClient(t, server)
		if err := c.DeleteSecret(t.Context(), "urn:li:dataHubSecret:my-secret"); err != nil {
			t.Fatalf("DeleteSecret() error = %v", err)
		}
	})

	t.Run("not_found_is_idempotent", func(t *testing.T) {
		handler := &graphqlHandler{
			deleteResp: func(_ string) any {
				return map[string]any{"errors": []map[string]any{{"message": "Secret not found"}}}
			},
		}
		server := httptest.NewServer(handler)
		defer server.Close()

		c := newTestClient(t, server)
		// "not found" on delete must not return an error.
		if err := c.DeleteSecret(t.Context(), "urn:li:dataHubSecret:gone"); err != nil {
			t.Fatalf("DeleteSecret() should be idempotent for not-found, got error = %v", err)
		}
	})

	t.Run("empty_urn_returns_error", func(t *testing.T) {
		c := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})))
		if err := c.DeleteSecret(t.Context(), ""); err == nil {
			t.Fatal("expected error for empty URN, got nil")
		}
	})

	t.Run("other_graphql_error_returns_error", func(t *testing.T) {
		handler := &graphqlHandler{
			deleteResp: func(_ string) any {
				return map[string]any{"errors": []map[string]any{{"message": "internal server error"}}}
			},
		}
		server := httptest.NewServer(handler)
		defer server.Close()

		c := newTestClient(t, server)
		err := c.DeleteSecret(t.Context(), "urn:li:dataHubSecret:x")
		if err == nil {
			t.Fatal("expected error for non-not-found GraphQL error, got nil")
		}
	})
}
