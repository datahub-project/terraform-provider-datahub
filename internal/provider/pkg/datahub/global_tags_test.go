// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// globalTagsMockServer serves a single entity whose globalTags aspect is
// stored on POST and served on GET, mimicking OpenAPI v3 semantics.
func globalTagsMockServer(t *testing.T, urn string, persist bool) (*httptest.Server, *[]string) {
	t.Helper()
	stored := &[]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			var payload []struct {
				URN        string `json:"urn"`
				GlobalTags struct {
					Value struct {
						Tags []struct {
							Tag string `json:"tag"`
						} `json:"tags"`
					} `json:"value"`
				} `json:"globalTags"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decoding POST payload: %v", err)
			}
			if persist {
				tags := []string{}
				for _, e := range payload {
					for _, tg := range e.GlobalTags.Value.Tags {
						tags = append(tags, tg.Tag)
					}
				}
				*stored = tags
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("[]"))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, urn):
			tags := make([]map[string]string, 0, len(*stored))
			for _, tg := range *stored {
				tags = append(tags, map[string]string{"tag": tg})
			}
			resp := map[string]any{
				"urn": urn,
				"globalTags": map[string]any{
					"value": map[string]any{"tags": tags},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return server, stored
}

func TestSetGlobalTags(t *testing.T) {
	const urn = "urn:li:corpuser:alice"

	t.Run("write_and_readback", func(t *testing.T) {
		server, stored := globalTagsMockServer(t, urn, true)
		defer server.Close()
		c := newTestClient(t, server)

		err := c.SetGlobalTags(t.Context(), "corpuser", urn, []string{"urn:li:tag:b", "urn:li:tag:a"})
		if err != nil {
			t.Fatalf("SetGlobalTags() error = %v", err)
		}
		if len(*stored) != 2 || (*stored)[0] != "urn:li:tag:a" {
			t.Errorf("stored = %v, want sorted [a b]", *stored)
		}
	})

	t.Run("clear", func(t *testing.T) {
		server, stored := globalTagsMockServer(t, urn, true)
		defer server.Close()
		*stored = []string{"urn:li:tag:old"}
		c := newTestClient(t, server)

		if err := c.SetGlobalTags(t.Context(), "corpuser", urn, nil); err != nil {
			t.Fatalf("SetGlobalTags(clear) error = %v", err)
		}
		if len(*stored) != 0 {
			t.Errorf("stored = %v, want empty", *stored)
		}
	})

	t.Run("silent_noop_detected", func(t *testing.T) {
		// Server returns 200 but never persists: the CAT-2562 failure mode.
		server, _ := globalTagsMockServer(t, urn, false)
		defer server.Close()
		c := newTestClient(t, server)

		err := c.SetGlobalTags(t.Context(), "corpuser", urn, []string{"urn:li:tag:a"})
		if err == nil || !strings.Contains(err.Error(), "did not persist") {
			t.Fatalf("expected silent-noop error, got %v", err)
		}
	})

	t.Run("http_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()
		c := newTestClient(t, server)

		if err := c.SetGlobalTags(t.Context(), "corpuser", urn, []string{"urn:li:tag:a"}); err == nil {
			t.Fatal("expected error for 403")
		}
	})
}

func TestGetGlobalTags(t *testing.T) {
	const urn = "urn:li:dataProduct:dp1"

	t.Run("present", func(t *testing.T) {
		server, stored := globalTagsMockServer(t, urn, true)
		defer server.Close()
		*stored = []string{"urn:li:tag:z", "urn:li:tag:a"}
		c := newTestClient(t, server)

		tags, found, err := c.GetGlobalTags(t.Context(), "dataproduct", urn)
		if err != nil || !found {
			t.Fatalf("GetGlobalTags() = %v, %v, %v", tags, found, err)
		}
		if len(tags) != 2 || tags[0] != "urn:li:tag:a" {
			t.Errorf("tags = %v, want sorted [a z]", tags)
		}
	})

	t.Run("aspect_absent", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"urn":"` + urn + `"}`))
		}))
		defer server.Close()
		c := newTestClient(t, server)

		tags, found, err := c.GetGlobalTags(t.Context(), "dataproduct", urn)
		if err != nil || !found {
			t.Fatalf("GetGlobalTags() = %v, %v, %v", tags, found, err)
		}
		if len(tags) != 0 {
			t.Errorf("tags = %v, want empty", tags)
		}
	})

	t.Run("not_found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()
		c := newTestClient(t, server)

		_, found, err := c.GetGlobalTags(t.Context(), "dataproduct", urn)
		if err != nil || found {
			t.Fatalf("expected not-found, got found=%v err=%v", found, err)
		}
	})
}
