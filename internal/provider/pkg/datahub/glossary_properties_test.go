// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// glossaryInfoMockServer stores the info aspect value from the last POST and
// serves it on GET. A nil stored value means the entity does not exist.
func glossaryInfoMockServer(t *testing.T, urn, aspectName string, stored *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var payload []map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decoding POST payload: %v", err)
			}
			var aspect struct {
				Value map[string]any `json:"value"`
			}
			if err := json.Unmarshal(payload[0][aspectName], &aspect); err != nil {
				t.Errorf("decoding %s: %v", aspectName, err)
			}
			*stored = aspect.Value
			_, _ = w.Write([]byte("[]"))
		case http.MethodGet:
			if *stored == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"urn":      urn,
				aspectName: map[string]any{"value": *stored},
			})
		}
	}))
}

func TestSetGlossaryTermProperties(t *testing.T) {
	const urn = "urn:li:glossaryTerm:revenue"

	t.Run("preserves_unmanaged_fields", func(t *testing.T) {
		stored := map[string]any{
			"id":               "Finance.Revenue",
			"name":             "Revenue",
			"definition":       "old",
			"parentNode":       "urn:li:glossaryNode:old",
			"termSource":       "EXTERNAL",
			"sourceRef":        "FIBO",
			"sourceUrl":        "https://example.com/revenue",
			"customProperties": map[string]any{"team": "a"},
		}
		server := glossaryInfoMockServer(t, urn, "glossaryTermInfo", &stored)
		defer server.Close()
		c := newTestClient(t, server)

		err := c.SetGlossaryTermProperties(t.Context(), urn, "Revenue v2", "new", "", map[string]string{"team": "b"})
		if err != nil {
			t.Fatalf("SetGlossaryTermProperties() error = %v", err)
		}

		if stored["name"] != "Revenue v2" || stored["definition"] != "new" {
			t.Errorf("name/definition = %v/%v, want Revenue v2/new", stored["name"], stored["definition"])
		}
		if _, ok := stored["parentNode"]; ok {
			t.Errorf("parentNode = %v, want it cleared", stored["parentNode"])
		}
		if got := mustJSON(t, stored["customProperties"]); got != `{"team":"b"}` {
			t.Errorf("customProperties = %s, want {\"team\":\"b\"}", got)
		}
		want := map[string]string{
			"id":         "Finance.Revenue",
			"termSource": "EXTERNAL",
			"sourceRef":  "FIBO",
			"sourceUrl":  "https://example.com/revenue",
		}
		for k, v := range want {
			if stored[k] != v {
				t.Errorf("%s = %v, want %s preserved", k, stored[k], v)
			}
		}
	})

	t.Run("defaults_term_source_when_absent", func(t *testing.T) {
		var stored map[string]any
		server := glossaryInfoMockServer(t, urn, "glossaryTermInfo", &stored)
		defer server.Close()
		c := newTestClient(t, server)

		err := c.SetGlossaryTermProperties(t.Context(), urn, "Revenue", "desc", "urn:li:glossaryNode:finance", map[string]string{"team": "a"})
		if err != nil {
			t.Fatalf("SetGlossaryTermProperties() error = %v", err)
		}
		if stored["termSource"] != "INTERNAL" {
			t.Errorf("termSource = %v, want INTERNAL", stored["termSource"])
		}
		if stored["parentNode"] != "urn:li:glossaryNode:finance" {
			t.Errorf("parentNode = %v, want urn:li:glossaryNode:finance", stored["parentNode"])
		}
	})
}

func TestSetGlossaryNodeProperties_PreservesID(t *testing.T) {
	const urn = "urn:li:glossaryNode:finance"
	stored := map[string]any{
		"id":         "Finance",
		"name":       "Finance",
		"definition": "old",
	}
	server := glossaryInfoMockServer(t, urn, "glossaryNodeInfo", &stored)
	defer server.Close()
	c := newTestClient(t, server)

	if err := c.SetGlossaryNodeProperties(t.Context(), urn, "Finance", "new", "", nil); err != nil {
		t.Fatalf("SetGlossaryNodeProperties() error = %v", err)
	}
	if stored["id"] != "Finance" {
		t.Errorf("id = %v, want Finance preserved", stored["id"])
	}
	if _, ok := stored["termSource"]; ok {
		t.Errorf("termSource = %v, want absent on a node", stored["termSource"])
	}
}
