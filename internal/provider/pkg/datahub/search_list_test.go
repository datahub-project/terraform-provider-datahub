// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestListURNsByEntityTypeSkipsSearchCache pins that the search-backed list
// helper asks DataHub to bypass its search result cache. Without the flag a
// wildcard search keeps returning the result cached by an earlier identical
// query, so an entity created moments ago stays out of every list data source
// and import enumeration for minutes (measured at over a minute on DataHub
// Cloud), however often the caller retries.
func TestListURNsByEntityTypeSkipsSearchCache(t *testing.T) {
	var flags map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables struct {
				Input map[string]any `json:"input"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		flags, _ = req.Variables.Input["searchFlags"].(map[string]any)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"searchAcrossEntities":{"total":1,"searchResults":[{"entity":{"urn":"urn:li:tag:x"}}]}}}`))
	}))
	defer server.Close()

	urns, err := newTestClient(t, server).ListTagURNs(t.Context())
	if err != nil {
		t.Fatalf("ListTagURNs: %v", err)
	}
	if len(urns) != 1 || urns[0] != "urn:li:tag:x" {
		t.Fatalf("urns = %v, want [urn:li:tag:x]", urns)
	}
	if flags["skipCache"] != true {
		t.Errorf("searchFlags = %v, want skipCache: true", flags)
	}
}
