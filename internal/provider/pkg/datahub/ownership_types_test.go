// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWriteOwnershipTypeInfoCreated checks that created keeps recording when
// the type was made: a stored stamp is sent back unchanged, and one that is
// absent or null is replaced with the time of this write, since the aspect
// requires it.
func TestWriteOwnershipTypeInfoCreated(t *testing.T) {
	stored := map[string]any{"time": float64(1700000000000), "actor": "urn:li:corpuser:creator"}
	cases := []struct {
		name    string
		info    map[string]any
		wantOld bool
	}{
		{name: "kept", info: map[string]any{"name": "Old", "created": stored}, wantOld: true},
		{name: "absent", info: map[string]any{"name": "Old"}},
		{name: "null", info: map[string]any{"name": "Old", "created": nil}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sent map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"ownershipTypeInfo": map[string]any{"value": tc.info},
					})
					return
				}
				var body []struct {
					Info struct {
						Value map[string]any `json:"value"`
					} `json:"ownershipTypeInfo"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 1 {
					t.Errorf("decoding POST: %v (%d entities)", err, len(body))
				} else {
					sent = body[0].Info.Value
				}
				_, _ = w.Write([]byte("[]"))
			}))
			defer server.Close()

			if err := newTestClient(t, server).WriteOwnershipTypeInfo(t.Context(), "urn:li:ownershipType:t", "New", ""); err != nil {
				t.Fatalf("WriteOwnershipTypeInfo: %v", err)
			}
			created, ok := sent["created"].(map[string]any)
			if !ok {
				t.Fatalf("POST carried no created stamp: %v", sent)
			}
			if gotOld := created["actor"] == stored["actor"]; gotOld != tc.wantOld {
				t.Errorf("created = %v, want stored stamp kept: %t", created, tc.wantOld)
			}
		})
	}
}
