// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
				"dataProductProperties": map[string]any{"value": *stored},
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
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
