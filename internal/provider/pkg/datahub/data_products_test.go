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

// dataProductMockServer stores the dataProductProperties value from the last
// POST and serves it on GET. A nil stored value means the entity does not exist.
func dataProductMockServer(t *testing.T, urn string, stored *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var payload []struct {
				Props struct {
					Value map[string]any `json:"value"`
				} `json:"dataProductProperties"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decoding POST payload: %v", err)
			}
			*stored = payload[0].Props.Value
			_, _ = w.Write([]byte("[]"))
		case http.MethodGet:
			if *stored == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"urn":                   urn,
				"dataProductProperties": map[string]any{"value": *stored, "systemMetadata": map[string]any{"version": "1"}},
			})
		}
	}))
}

func TestWriteDataProductProperties(t *testing.T) {
	const urn = "urn:li:dataProduct:orders"
	assets := []any{
		map[string]any{"destinationUrn": "urn:li:dataset:(urn:li:dataPlatform:hive,db.orders,PROD)"},
	}

	t.Run("preserves_unmanaged_fields", func(t *testing.T) {
		stored := map[string]any{
			"name":              "Orders",
			"description":       "old",
			"externalUrl":       "https://example.com/old",
			"customProperties":  map[string]any{"team": "a"},
			"assets":            assets,
			"parentDataProduct": "urn:li:dataProduct:sales",
		}
		server := dataProductMockServer(t, urn, &stored)
		defer server.Close()
		c := newTestClient(t, server)

		if err := c.WriteDataProductProperties(t.Context(), urn, "Orders v2", "", "", nil, ""); err != nil {
			t.Fatalf("WriteDataProductProperties() error = %v", err)
		}

		if stored["name"] != "Orders v2" {
			t.Errorf("name = %v, want Orders v2", stored["name"])
		}
		for _, k := range []string{"description", "externalUrl", "customProperties"} {
			if _, ok := stored[k]; ok {
				t.Errorf("%s = %v, want it cleared", k, stored[k])
			}
		}
		if got, _ := json.Marshal(stored["assets"]); string(got) != mustJSON(t, assets) {
			t.Errorf("assets = %s, want %s", got, mustJSON(t, assets))
		}
		if stored["parentDataProduct"] != "urn:li:dataProduct:sales" {
			t.Errorf("parentDataProduct = %v, want it preserved", stored["parentDataProduct"])
		}
	})

	t.Run("creates_when_absent", func(t *testing.T) {
		var stored map[string]any
		server := dataProductMockServer(t, urn, &stored)
		defer server.Close()
		c := newTestClient(t, server)

		err := c.WriteDataProductProperties(t.Context(), urn, "Orders", "desc", "", map[string]string{"team": "a"}, "")
		if err != nil {
			t.Fatalf("WriteDataProductProperties() error = %v", err)
		}
		if stored["name"] != "Orders" || stored["description"] != "desc" {
			t.Errorf("stored = %v, want name and description written", stored)
		}
		if _, ok := stored["assets"]; ok {
			t.Errorf("assets = %v, want absent on create", stored["assets"])
		}
	})

	// An entity can exist without this aspect, e.g. with only its key aspect.
	// The read then succeeds with nothing to preserve, and the write must go
	// ahead with just the managed fields rather than failing.
	t.Run("entity_without_aspect", func(t *testing.T) {
		var posted map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"urn":"` + urn + `","dataProductKey":{"value":{"id":"orders"}}}`))
			case http.MethodPost:
				var payload []struct {
					Props struct {
						Value map[string]any `json:"value"`
					} `json:"dataProductProperties"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decoding POST payload: %v", err)
				}
				posted = payload[0].Props.Value
				_, _ = w.Write([]byte("[]"))
			}
		}))
		defer server.Close()
		c := newTestClient(t, server)

		if err := c.WriteDataProductProperties(t.Context(), urn, "Orders", "desc", "", nil, ""); err != nil {
			t.Fatalf("WriteDataProductProperties() error = %v", err)
		}
		if posted["name"] != "Orders" || posted["description"] != "desc" {
			t.Errorf("posted = %v, want name and description written", posted)
		}
	})
}

// TestWriteDataProductPropertiesReadFailure pins the safety property the
// read-before-write depends on: when the current aspect cannot be read, the
// write must not happen. Treating a failed read as an empty aspect would POST
// only the managed fields and erase the assets the read exists to protect, so
// every failure mode here must return an error and send no POST.
func TestWriteDataProductPropertiesReadFailure(t *testing.T) {
	const urn = "urn:li:dataProduct:orders"

	cases := []struct {
		name    string
		status  int
		body    string
		hangup  bool // drop the connection instead of answering the GET
		wantErr string
	}{
		{name: "connection_dropped", hangup: true, wantErr: "data product read request failed"},
		{name: "server_error", status: http.StatusInternalServerError, body: `{"error":"boom"}`, wantErr: "HTTP 500"},
		{name: "bad_gateway", status: http.StatusBadGateway, body: "upstream down", wantErr: "HTTP 502"},
		{name: "unauthorized", status: http.StatusUnauthorized, wantErr: "HTTP 401"},
		{name: "forbidden", status: http.StatusForbidden, wantErr: "HTTP 403"},
		{name: "malformed_body", status: http.StatusOK, body: `{"urn":`, wantErr: "parsing data product response"},
		{name: "aspect_not_an_object", status: http.StatusOK, body: `{"urn":"` + urn + `","dataProductProperties":{"value":"oops"}}`, wantErr: "parsing dataProductProperties aspect"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var posts int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					if tc.hangup {
						hj, ok := w.(http.Hijacker)
						if !ok {
							t.Error("response writer does not support hijacking")
							return
						}
						conn, _, err := hj.Hijack()
						if err != nil {
							t.Errorf("hijacking GET connection: %v", err)
							return
						}
						_ = conn.Close()
						return
					}
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.body))
				case http.MethodPost:
					posts++
					_, _ = w.Write([]byte("[]"))
				}
			}))
			defer server.Close()
			c := newTestClient(t, server)

			err := c.WriteDataProductProperties(t.Context(), urn, "Orders", "", "", nil, "")
			if err == nil {
				t.Fatal("WriteDataProductProperties() error = nil, want an error when the current aspect cannot be read")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
			}
			if posts != 0 {
				t.Errorf("sent %d POST(s) after a failed read, want 0: the write would erase fields the read was meant to preserve", posts)
			}
		})
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
