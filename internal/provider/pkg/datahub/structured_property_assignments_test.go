// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestSetStructuredPropertyValues_SerializesPerEntity is the CAT-2568 regression
// guard. The fake server reproduces DataHub's non-atomic read-modify-write merge
// of an entity's single structuredProperties aspect (read a snapshot, yield,
// then write the snapshot back with the new property added). Without per-entity
// serialization, concurrent upserts for different properties on the same entity
// race and lose updates; the lock in SetStructuredPropertyValues must serialize
// them so every write lands.
func TestSetStructuredPropertyValues_SerializesPerEntity(t *testing.T) {
	var mu sync.Mutex
	store := map[string]map[string]bool{} // entityURN -> set of assigned property URNs

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Variables struct {
				Input struct {
					AssetURN string `json:"assetUrn"`
					Params   []struct {
						StructuredPropertyURN string `json:"structuredPropertyUrn"`
					} `json:"structuredPropertyInputParams"`
				} `json:"input"`
			} `json:"variables"`
		}
		_ = json.Unmarshal(body, &req)
		asset := req.Variables.Input.AssetURN
		prop := ""
		if len(req.Variables.Input.Params) > 0 {
			prop = req.Variables.Input.Params[0].StructuredPropertyURN
		}

		// Non-atomic merge with a yield window -- the CAT-2568 race.
		mu.Lock()
		snapshot := map[string]bool{}
		for k := range store[asset] {
			snapshot[k] = true
		}
		mu.Unlock()

		time.Sleep(2 * time.Millisecond)

		snapshot[prop] = true
		mu.Lock()
		store[asset] = snapshot
		mu.Unlock()

		_, _ = w.Write([]byte(`{"data":{"upsertStructuredProperties":{"properties":[]}}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	const entity = "urn:li:domain:cat2568-race"
	props := []string{
		"urn:li:structuredProperty:a",
		"urn:li:structuredProperty:b",
		"urn:li:structuredProperty:c",
		"urn:li:structuredProperty:d",
		"urn:li:structuredProperty:e",
	}

	var wg sync.WaitGroup
	for _, p := range props {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			if err := c.SetStructuredPropertyValues(context.Background(), entity, p, "string", []string{"x"}); err != nil {
				t.Errorf("SetStructuredPropertyValues(%s): %v", p, err)
			}
		}(p)
	}
	wg.Wait()

	mu.Lock()
	got := len(store[entity])
	mu.Unlock()
	if got != len(props) {
		t.Fatalf("lost updates: entity has %d/%d properties -- per-entity serialization (CAT-2568 workaround) is not holding", got, len(props))
	}
}
