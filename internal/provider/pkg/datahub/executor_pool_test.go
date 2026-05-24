// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ossGraphQLHandler returns a handler that responds with the FieldUndefined
// error an OSS DataHub GraphQL endpoint returns for unknown operations.
func ossGraphQLHandler(operation string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errors": []map[string]any{
				{
					"message": "Validation error of type FieldUndefined: Field '" +
						operation + "' in type 'Mutation' is undefined @ '" + operation + "'",
				},
			},
		})
	})
}

func TestIsCloudOnlyError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{
			"Validation error of type FieldUndefined: Field 'createRemoteExecutorPool' in type 'Mutation' is undefined",
			true,
		},
		{
			"Validation error of type FieldUndefined: Field 'getRemoteExecutorPool' in type 'Query' is undefined @ 'getRemoteExecutorPool'",
			true,
		},
		{
			"RemoteExecutorPool type is undefined",
			true,
		},
		// UnknownType: graphql-java error when the input type is absent from the schema.
		// Seen on Quickstart v1.5.0.6 for createRemoteExecutorPool / updateRemoteExecutorPool.
		{
			"Validation error (UnknownType) : Unknown type 'CreateRemoteExecutorPoolInput'",
			true,
		},
		{
			"Validation error (UnknownType) : Unknown type 'UpdateRemoteExecutorPoolInput'",
			true,
		},
		// UnknownType on an unrelated type should not be treated as Cloud-only.
		{"Validation error (UnknownType) : Unknown type 'SomethingElse'", false},
		// FieldUndefined on a sub-field of a result type must NOT be treated as Cloud-only.
		// This happens on Cloud builds that don't expose a particular field (e.g. 'channel').
		{
			"Validation error of type FieldUndefined: Field 'channel' in type 'RemoteExecutorPool' is undefined @ 'getRemoteExecutorPool/channel'",
			false,
		},
		{"permission denied", false},
		{"internal server error", false},
		{"executor pool my-pool already exists", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isCloudOnlyError(tc.msg); got != tc.want {
			t.Errorf("isCloudOnlyError(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestValidatePoolID(t *testing.T) {
	cases := []struct {
		id      string
		wantErr bool
		errFrag string
	}{
		{"", true, "empty"},
		{"my-pool", false, ""},
		{"my.pool", false, ""},
		{"my_pool", false, ""},
		{"MyPool123", false, ""},
		{"has space", true, "alphanumeric"},
		{"has/slash", true, "alphanumeric"},
		{"has@at", true, "alphanumeric"},
		{"default", true, "reserved"},
		{"embedded", true, "reserved"},
	}
	for _, tc := range cases {
		err := ValidatePoolID(tc.id)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidatePoolID(%q) error = %v, wantErr %v", tc.id, err, tc.wantErr)
			continue
		}
		if tc.wantErr && tc.errFrag != "" && !strings.Contains(strings.ToLower(err.Error()), tc.errFrag) {
			t.Errorf("ValidatePoolID(%q) error = %q, want to contain %q", tc.id, err.Error(), tc.errFrag)
		}
	}
}

func TestCreateRemoteExecutorPool(t *testing.T) {
	t.Run("oss_FieldUndefined_returns_ErrExecutorPoolCloudOnly", func(t *testing.T) {
		srv := httptest.NewServer(ossGraphQLHandler("createRemoteExecutorPool"))
		defer srv.Close()
		c := newTestClient(t, srv)
		_, err := c.CreateRemoteExecutorPool(t.Context(), CreateRemoteExecutorPoolInput{PoolID: "test-pool"})
		if !errors.Is(err, ErrExecutorPoolCloudOnly) {
			t.Fatalf("error = %v, want ErrExecutorPoolCloudOnly", err)
		}
	})

	t.Run("success_returns_urn", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"createRemoteExecutorPool": "urn:li:dataHubRemoteExecutorPool:test-pool",
				},
			})
		}))
		defer srv.Close()
		c := newTestClient(t, srv)
		urn, err := c.CreateRemoteExecutorPool(t.Context(), CreateRemoteExecutorPoolInput{PoolID: "test-pool"})
		if err != nil {
			t.Fatalf("CreateRemoteExecutorPool() error = %v", err)
		}
		if urn != "urn:li:dataHubRemoteExecutorPool:test-pool" {
			t.Errorf("URN = %q, want urn:li:dataHubRemoteExecutorPool:test-pool", urn)
		}
	})

	t.Run("already_exists_error_includes_import_hint", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]any{{"message": "executor pool my-pool already exists"}},
			})
		}))
		defer srv.Close()
		c := newTestClient(t, srv)
		_, err := c.CreateRemoteExecutorPool(t.Context(), CreateRemoteExecutorPoolInput{PoolID: "my-pool"})
		if err == nil {
			t.Fatal("expected error for duplicate pool, got nil")
		}
		if !strings.Contains(err.Error(), "import") {
			t.Errorf("error = %q, expected import hint", err.Error())
		}
	})

	t.Run("http_401_returns_error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()
		c := newTestClient(t, srv)
		_, err := c.CreateRemoteExecutorPool(t.Context(), CreateRemoteExecutorPoolInput{PoolID: "test-pool"})
		if err == nil {
			t.Fatal("expected error for 401, got nil")
		}
	})
}

func TestGetRemoteExecutorPoolByURN(t *testing.T) {
	t.Run("oss_FieldUndefined_returns_ErrExecutorPoolCloudOnly", func(t *testing.T) {
		srv := httptest.NewServer(ossGraphQLHandler("getRemoteExecutorPool"))
		defer srv.Close()
		c := newTestClient(t, srv)
		_, err := c.GetRemoteExecutorPoolByURN(t.Context(), "urn:li:dataHubRemoteExecutorPool:test-pool")
		if !errors.Is(err, ErrExecutorPoolCloudOnly) {
			t.Fatalf("error = %v, want ErrExecutorPoolCloudOnly", err)
		}
	})

	t.Run("not_found_returns_nil", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"getRemoteExecutorPool": nil},
			})
		}))
		defer srv.Close()
		c := newTestClient(t, srv)
		pool, err := c.GetRemoteExecutorPoolByURN(t.Context(), "urn:li:dataHubRemoteExecutorPool:missing")
		if err != nil {
			t.Fatalf("unexpected error = %v", err)
		}
		if pool != nil {
			t.Errorf("expected nil for not-found pool, got %+v", pool)
		}
	})

	t.Run("found_returns_pool_with_all_fields", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"getRemoteExecutorPool": map[string]any{
						"urn":            "urn:li:dataHubRemoteExecutorPool:my-pool",
						"executorPoolId": "my-pool",
						"description":    "test pool",
						"isDefault":      true,
						"isEmbedded":     false,
						"createdAt":      int64(1716000000000),
						"state":          map[string]any{"status": "READY", "message": ""},
					},
				},
			})
		}))
		defer srv.Close()
		c := newTestClient(t, srv)
		pool, err := c.GetRemoteExecutorPoolByURN(t.Context(), "urn:li:dataHubRemoteExecutorPool:my-pool")
		if err != nil {
			t.Fatalf("GetRemoteExecutorPoolByURN() error = %v", err)
		}
		if pool == nil {
			t.Fatal("pool is nil, want non-nil")
		}
		if pool.PoolID != "my-pool" {
			t.Errorf("PoolID = %q, want my-pool", pool.PoolID)
		}
		if !pool.IsDefault {
			t.Error("IsDefault = false, want true")
		}
		if pool.StateStatus != "READY" {
			t.Errorf("StateStatus = %q, want READY", pool.StateStatus)
		}
	})

	t.Run("empty_urn_returns_error", func(t *testing.T) {
		c := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})))
		_, err := c.GetRemoteExecutorPoolByURN(t.Context(), "")
		if err == nil {
			t.Fatal("expected error for empty URN, got nil")
		}
	})
}

func TestDeleteRemoteExecutorPool(t *testing.T) {
	t.Run("success_on_204", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()
		c := newTestClient(t, srv)
		if err := c.DeleteRemoteExecutorPool(t.Context(), "urn:li:dataHubRemoteExecutorPool:my-pool"); err != nil {
			t.Fatalf("DeleteRemoteExecutorPool() error = %v", err)
		}
	})

	t.Run("not_found_is_idempotent", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.NotFound(w, nil)
		}))
		defer srv.Close()
		c := newTestClient(t, srv)
		if err := c.DeleteRemoteExecutorPool(t.Context(), "urn:li:dataHubRemoteExecutorPool:gone"); err != nil {
			t.Fatalf("DeleteRemoteExecutorPool() should be idempotent for 404, got error = %v", err)
		}
	})

	t.Run("empty_urn_returns_error", func(t *testing.T) {
		c := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})))
		if err := c.DeleteRemoteExecutorPool(t.Context(), ""); err == nil {
			t.Fatal("expected error for empty URN, got nil")
		}
	})

	t.Run("server_error_returns_error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("something went wrong"))
		}))
		defer srv.Close()
		c := newTestClient(t, srv)
		err := c.DeleteRemoteExecutorPool(t.Context(), "urn:li:dataHubRemoteExecutorPool:my-pool")
		if err == nil {
			t.Fatal("expected error for 500, got nil")
		}
	})
}

func TestUpdateRemoteExecutorPool(t *testing.T) {
	t.Run("success_returns_nil", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"updateRemoteExecutorPool": true},
			})
		}))
		defer srv.Close()
		c := newTestClient(t, srv)
		desc := "new description"
		if err := c.UpdateRemoteExecutorPool(t.Context(), UpdateRemoteExecutorPoolInput{
			URN:         "urn:li:dataHubRemoteExecutorPool:my-pool",
			Description: &desc,
		}); err != nil {
			t.Fatalf("UpdateRemoteExecutorPool() error = %v", err)
		}
	})

	t.Run("oss_FieldUndefined_returns_ErrExecutorPoolCloudOnly", func(t *testing.T) {
		srv := httptest.NewServer(ossGraphQLHandler("updateRemoteExecutorPool"))
		defer srv.Close()
		c := newTestClient(t, srv)
		if err := c.UpdateRemoteExecutorPool(t.Context(), UpdateRemoteExecutorPoolInput{
			URN: "urn:li:dataHubRemoteExecutorPool:my-pool",
		}); !errors.Is(err, ErrExecutorPoolCloudOnly) {
			t.Fatalf("error = %v, want ErrExecutorPoolCloudOnly", err)
		}
	})

	t.Run("http_401_returns_error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()
		c := newTestClient(t, srv)
		err := c.UpdateRemoteExecutorPool(t.Context(), UpdateRemoteExecutorPoolInput{
			URN: "urn:li:dataHubRemoteExecutorPool:my-pool",
		})
		if err == nil {
			t.Fatal("expected error for 401, got nil")
		}
	})

	t.Run("empty_urn_returns_error", func(t *testing.T) {
		c := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})))
		if err := c.UpdateRemoteExecutorPool(t.Context(), UpdateRemoteExecutorPoolInput{}); err == nil {
			t.Fatal("expected error for empty URN, got nil")
		}
	})
}

func TestSetDefaultRemoteExecutorPool(t *testing.T) {
	t.Run("success_returns_nil", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"updateDefaultRemoteExecutorPool": true},
			})
		}))
		defer srv.Close()
		c := newTestClient(t, srv)
		if err := c.SetDefaultRemoteExecutorPool(t.Context(), "urn:li:dataHubRemoteExecutorPool:my-pool"); err != nil {
			t.Fatalf("SetDefaultRemoteExecutorPool() error = %v", err)
		}
	})

	t.Run("oss_FieldUndefined_returns_ErrExecutorPoolCloudOnly", func(t *testing.T) {
		srv := httptest.NewServer(ossGraphQLHandler("updateDefaultRemoteExecutorPool"))
		defer srv.Close()
		c := newTestClient(t, srv)
		if err := c.SetDefaultRemoteExecutorPool(t.Context(), "urn:li:dataHubRemoteExecutorPool:my-pool"); !errors.Is(err, ErrExecutorPoolCloudOnly) {
			t.Fatalf("error = %v, want ErrExecutorPoolCloudOnly", err)
		}
	})

	t.Run("http_401_returns_error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()
		c := newTestClient(t, srv)
		if err := c.SetDefaultRemoteExecutorPool(t.Context(), "urn:li:dataHubRemoteExecutorPool:my-pool"); err == nil {
			t.Fatal("expected error for 401, got nil")
		}
	})

	t.Run("empty_urn_returns_error", func(t *testing.T) {
		c := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})))
		if err := c.SetDefaultRemoteExecutorPool(t.Context(), ""); err == nil {
			t.Fatal("expected error for empty URN, got nil")
		}
	})
}

func TestWaitForRemoteExecutorPoolReady(t *testing.T) {
	t.Run("provisioning_failed_returns_error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"getRemoteExecutorPool": map[string]any{
						"urn":            "urn:li:dataHubRemoteExecutorPool:my-pool",
						"executorPoolId": "my-pool",
						"state":          map[string]any{"status": "PROVISIONING_FAILED", "message": "SQS queue creation failed"},
					},
				},
			})
		}))
		defer srv.Close()
		c := newTestClient(t, srv)
		_, err := c.WaitForRemoteExecutorPoolReady(t.Context(), "urn:li:dataHubRemoteExecutorPool:my-pool", 0)
		if err == nil {
			t.Fatal("expected error for PROVISIONING_FAILED, got nil")
		}
		if !strings.Contains(err.Error(), "provisioning failed") {
			t.Errorf("error = %q, want to contain 'provisioning failed'", err.Error())
		}
		if !strings.Contains(err.Error(), "SQS queue creation failed") {
			t.Errorf("error = %q, want to contain state message", err.Error())
		}
	})

	t.Run("timeout_returns_error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"getRemoteExecutorPool": map[string]any{
						"urn":            "urn:li:dataHubRemoteExecutorPool:my-pool",
						"executorPoolId": "my-pool",
						"state":          map[string]any{"status": "PROVISIONING_IN_PROGRESS", "message": ""},
					},
				},
			})
		}))
		defer srv.Close()
		c := newTestClient(t, srv)
		_, err := c.WaitForRemoteExecutorPoolReady(t.Context(), "urn:li:dataHubRemoteExecutorPool:my-pool", time.Millisecond)
		if err == nil {
			t.Fatal("expected timeout error, got nil")
		}
		if !strings.Contains(err.Error(), "did not reach READY") {
			t.Errorf("error = %q, want to contain 'did not reach READY'", err.Error())
		}
	})

	t.Run("ready_returns_pool", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"getRemoteExecutorPool": map[string]any{
						"urn":            "urn:li:dataHubRemoteExecutorPool:my-pool",
						"executorPoolId": "my-pool",
						"state":          map[string]any{"status": "READY", "message": ""},
					},
				},
			})
		}))
		defer srv.Close()
		c := newTestClient(t, srv)
		pool, err := c.WaitForRemoteExecutorPoolReady(t.Context(), "urn:li:dataHubRemoteExecutorPool:my-pool", 0)
		if err != nil {
			t.Fatalf("WaitForRemoteExecutorPoolReady() error = %v", err)
		}
		if pool == nil {
			t.Fatal("pool is nil, want non-nil")
		}
	})
}
