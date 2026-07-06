// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServiceAccountURNHelpers(t *testing.T) {
	if got := ServiceAccountURN("ci-bot"); got != "urn:li:corpuser:service_ci-bot" {
		t.Errorf("ServiceAccountURN = %q", got)
	}
	if got := ServiceAccountIDFromURN("urn:li:corpuser:service_ci-bot"); got != "ci-bot" {
		t.Errorf("ServiceAccountIDFromURN(full) = %q, want ci-bot", got)
	}
	if got := ServiceAccountIDFromURN("service_ci-bot"); got != "ci-bot" {
		t.Errorf("ServiceAccountIDFromURN(username) = %q, want ci-bot", got)
	}
}

func TestUpsertServiceAccount(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openapi/v3/entity/corpuser" {
			t.Errorf("path = %q, want /openapi/v3/entity/corpuser", r.URL.Path)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()

	c := newTestClient(t, server)
	urn, err := c.UpsertServiceAccount(t.Context(), "ci-bot", "CI Bot", "Automation")
	if err != nil {
		t.Fatalf("UpsertServiceAccount() error = %v", err)
	}
	if urn != "urn:li:corpuser:service_ci-bot" {
		t.Errorf("urn = %q, want urn:li:corpuser:service_ci-bot", urn)
	}

	// The POST body must carry the subTypes aspect with SERVICE_ACCOUNT and the
	// service_-prefixed username.
	var entities []map[string]json.RawMessage
	if err := json.Unmarshal(gotBody, &entities); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if len(entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(entities))
	}
	if _, ok := entities[0]["subTypes"]; !ok {
		t.Errorf("body missing subTypes aspect: %s", gotBody)
	}
	if !strings.Contains(string(gotBody), "SERVICE_ACCOUNT") {
		t.Errorf("body missing SERVICE_ACCOUNT: %s", gotBody)
	}
	if !strings.Contains(string(gotBody), "service_ci-bot") {
		t.Errorf("body missing service_ci-bot username: %s", gotBody)
	}
}

func TestGetServiceAccountByURN(t *testing.T) {
	newServer := func(subTypes []string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			entity := map[string]any{
				"urn":         "urn:li:corpuser:service_ci-bot",
				"corpUserKey": map[string]any{"value": map[string]any{"username": "service_ci-bot"}},
				"corpUserInfo": map[string]any{"value": map[string]any{
					"displayName": "CI Bot", "title": "Automation", "active": true,
				}},
			}
			if len(subTypes) > 0 {
				entity["subTypes"] = map[string]any{"value": map[string]any{"typeNames": subTypes}}
			}
			_ = json.NewEncoder(w).Encode(entity)
		}))
	}

	t.Run("is_service_account", func(t *testing.T) {
		server := newServer([]string{"SERVICE_ACCOUNT"})
		defer server.Close()
		c := newTestClient(t, server)
		sa, err := c.GetServiceAccountByURN(t.Context(), "urn:li:corpuser:service_ci-bot")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if sa == nil {
			t.Fatal("expected a service account, got nil")
		}
		if sa.InfoTitle != "Automation" {
			t.Errorf("InfoTitle = %q, want Automation", sa.InfoTitle)
		}
	})

	t.Run("not_a_service_account", func(t *testing.T) {
		server := newServer(nil) // corpUser without the subtype
		defer server.Close()
		c := newTestClient(t, server)
		sa, err := c.GetServiceAccountByURN(t.Context(), "urn:li:corpuser:service_faker")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if sa != nil {
			t.Errorf("expected nil for non-service-account corpUser, got %+v", sa)
		}
	})
}

func TestListServiceAccountURNs(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"listServiceAccounts": map[string]any{
					"total": 2,
					"serviceAccounts": []map[string]any{
						{"urn": "urn:li:corpuser:service_a"},
						{"urn": "urn:li:corpuser:service_b"},
					},
				}},
			})
		}))
		defer server.Close()
		c := newTestClient(t, server)
		urns, err := c.ListServiceAccountURNs(t.Context())
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(urns) != 2 || urns[0] != "urn:li:corpuser:service_a" {
			t.Errorf("urns = %v", urns)
		}
	})

	t.Run("unsupported", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]any{
					{"message": "Validation error (FieldUndefined): Field 'listServiceAccounts' in type 'Query' is undefined"},
				},
			})
		}))
		defer server.Close()
		c := newTestClient(t, server)
		_, err := c.ListServiceAccountURNs(t.Context())
		if !errors.Is(err, ErrServiceAccountsUnsupported) {
			t.Errorf("error = %v, want ErrServiceAccountsUnsupported", err)
		}
	})
}
