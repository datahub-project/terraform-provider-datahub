// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStructuredPropertySearchField(t *testing.T) {
	cases := []struct {
		name          string
		qualifiedName string
		version       string
		valueType     string
		want          string
	}{
		{
			name:          "unversioned",
			qualifiedName: "tf-example.governance.regions",
			want:          "structuredProperties.tf-example_governance_regions",
		},
		{
			name:          "unversioned_no_dots",
			qualifiedName: "regions",
			want:          "structuredProperties.regions",
		},
		{
			name:          "versioned",
			qualifiedName: "my.prop",
			version:       "20240614",
			valueType:     "string",
			want:          "structuredProperties._versioned.my_prop.20240614.string",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := structuredPropertySearchField(tc.qualifiedName, tc.version, tc.valueType)
			if got != tc.want {
				t.Errorf("structuredPropertySearchField(%q, %q, %q) = %q, want %q",
					tc.qualifiedName, tc.version, tc.valueType, got, tc.want)
			}
		})
	}
}

// shortenSettleBudget shrinks the settle-barrier tunables for the duration of
// a test and restores them afterwards.
func shortenSettleBudget(t *testing.T, timeout time.Duration) {
	t.Helper()
	prevTimeout, prevInterval := structuredPropertySettleTimeout, structuredPropertySettleInterval
	structuredPropertySettleTimeout = timeout
	structuredPropertySettleInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		structuredPropertySettleTimeout = prevTimeout
		structuredPropertySettleInterval = prevInterval
	})
}

// settleTestServer simulates the three endpoints DeleteStructuredProperty
// touches: the OpenAPI definition read, the settle-barrier search, and the
// delete mutation. searchTotals is consumed one element per search call; once
// exhausted the last element repeats.
type settleTestServer struct {
	mu           sync.Mutex
	definition   map[string]any // nil => respond 404
	searchTotals []int
	searchErr    bool // searchAcrossEntities returns a GraphQL error
	deleteErr    bool // deleteStructuredProperty returns a GraphQL error
	searchCalls  int
	searchFields []string
	deleteCalls  int
	deleteAfter  []int // snapshot of searchCalls at each delete
}

func (s *settleTestServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/structuredproperty/") {
			s.mu.Lock()
			def := s.definition
			s.mu.Unlock()
			if def == nil {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(def)
			return
		}

		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string `json:"query"`
			Variables struct {
				Input struct {
					OrFilters []struct {
						And []struct {
							Field     string `json:"field"`
							Condition string `json:"condition"`
						} `json:"and"`
					} `json:"orFilters"`
				} `json:"input"`
			} `json:"variables"`
		}
		_ = json.Unmarshal(body, &req)

		switch {
		case strings.Contains(req.Query, "searchAcrossEntities"):
			s.mu.Lock()
			if s.searchErr {
				s.searchCalls++
				s.mu.Unlock()
				_, _ = w.Write([]byte(`{"errors":[{"message":"search unavailable"}]}`))
				return
			}
			idx := s.searchCalls
			if idx >= len(s.searchTotals) {
				idx = len(s.searchTotals) - 1
			}
			total := s.searchTotals[idx]
			s.searchCalls++
			for _, of := range req.Variables.Input.OrFilters {
				for _, c := range of.And {
					if c.Condition == "EXISTS" {
						s.searchFields = append(s.searchFields, c.Field)
					}
				}
			}
			s.mu.Unlock()
			fmt.Fprintf(w, `{"data":{"searchAcrossEntities":{"total":%d}}}`, total)
		case strings.Contains(req.Query, "deleteStructuredProperty"):
			s.mu.Lock()
			s.deleteCalls++
			s.deleteAfter = append(s.deleteAfter, s.searchCalls)
			fails := s.deleteErr
			s.mu.Unlock()
			if fails {
				_, _ = w.Write([]byte(`{"errors":[{"message":"Unauthorized to delete this property"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"deleteStructuredProperty":true}}`))
		default:
			http.Error(w, `{"errors":[{"message":"unexpected query"}]}`, http.StatusBadRequest)
		}
	}
}

func spSettleDefinition(qualifiedName string) map[string]any {
	return map[string]any{
		"urn": "urn:li:structuredProperty:" + qualifiedName,
		"structuredPropertyKey": map[string]any{
			"value": map[string]any{"id": qualifiedName},
		},
		"propertyDefinition": map[string]any{
			"value": map[string]any{
				"qualifiedName": qualifiedName,
				"valueType":     "urn:li:dataType:datahub.string",
				"cardinality":   "SINGLE",
			},
		},
	}
}

// TestDeleteStructuredProperty_SettleBarrier is the CAT-2583 workaround guard:
// the delete mutation must not be issued while the search index still lists
// entities carrying the property, because the server-side
// PropertyDefinitionDeleteSideEffect patches every stale hit and resurrects
// concurrently hard-deleted entities. A single zero is not enough - index
// reads are not monotonic - so the barrier requires a streak of consecutive
// zeros, and a non-zero read resets the streak.
func TestDeleteStructuredProperty_SettleBarrier(t *testing.T) {
	shortenSettleBudget(t, 5*time.Second)

	// Totals include a zero followed by a stale non-zero read: the streak must
	// reset, so the delete may only fire after the trailing three zeros.
	ts := &settleTestServer{
		definition:   spSettleDefinition("tf-example.governance.regions"),
		searchTotals: []int{2, 0, 1, 0, 0, 0},
	}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.DeleteStructuredProperty(context.Background(), "urn:li:structuredProperty:tf-example.governance.regions"); err != nil {
		t.Fatalf("DeleteStructuredProperty: %v", err)
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.searchCalls != 6 {
		t.Errorf("expected 6 settle polls (streak reset by the stale non-zero), got %d", ts.searchCalls)
	}
	if ts.deleteCalls != 1 || len(ts.deleteAfter) != 1 || ts.deleteAfter[0] != 6 {
		t.Errorf("expected exactly one delete after the sixth poll, got deletes=%d after polls %v", ts.deleteCalls, ts.deleteAfter)
	}
	for _, f := range ts.searchFields {
		if f != "structuredProperties.tf-example_governance_regions" {
			t.Errorf("settle poll used field %q, want structuredProperties.tf-example_governance_regions", f)
		}
	}
}

// TestDeleteStructuredProperty_SettleTimeoutProceeds verifies the barrier is
// best-effort: when the index never settles within the budget, the delete
// still goes through rather than failing the destroy.
func TestDeleteStructuredProperty_SettleTimeoutProceeds(t *testing.T) {
	shortenSettleBudget(t, 20*time.Millisecond)

	ts := &settleTestServer{
		definition:   spSettleDefinition("tf-example.governance.tier"),
		searchTotals: []int{7},
	}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.DeleteStructuredProperty(context.Background(), "urn:li:structuredProperty:tf-example.governance.tier"); err != nil {
		t.Fatalf("DeleteStructuredProperty: %v", err)
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.deleteCalls != 1 {
		t.Errorf("expected delete to proceed after settle timeout, got deletes=%d", ts.deleteCalls)
	}
	if ts.searchCalls < 2 {
		t.Errorf("expected at least 2 settle polls before timing out, got %d", ts.searchCalls)
	}
}

// TestDeleteStructuredProperty_GuardsAndMutationError covers the input
// guards and verifies a delete-mutation failure surfaces as an error.
func TestDeleteStructuredProperty_GuardsAndMutationError(t *testing.T) {
	shortenSettleBudget(t, 5*time.Second)

	ts := &settleTestServer{
		definition: nil, // skip the barrier; this test targets the mutation path
		deleteErr:  true,
	}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.DeleteStructuredProperty(context.Background(), "  "); err == nil {
		t.Error("expected error for empty URN")
	}
	var nilClient *Client
	if err := nilClient.DeleteStructuredProperty(context.Background(), "urn:li:structuredProperty:x"); err == nil {
		t.Error("expected error for nil client")
	}
	err := c.DeleteStructuredProperty(context.Background(), "urn:li:structuredProperty:tf-example.governance.tier")
	if err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("expected the mutation error to surface, got %v", err)
	}
}

// TestDeleteStructuredProperty_SearchErrorProceeds verifies the barrier's
// best-effort contract from the other side: a failing settle query must
// abort the barrier and let the delete proceed, never fail the destroy.
func TestDeleteStructuredProperty_SearchErrorProceeds(t *testing.T) {
	shortenSettleBudget(t, 5*time.Second)

	ts := &settleTestServer{
		definition:   spSettleDefinition("tf-example.governance.tier"),
		searchTotals: []int{1},
		searchErr:    true,
	}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.DeleteStructuredProperty(context.Background(), "urn:li:structuredProperty:tf-example.governance.tier"); err != nil {
		t.Fatalf("DeleteStructuredProperty: %v", err)
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.searchCalls != 1 {
		t.Errorf("expected the barrier to abort after one failing poll, got %d", ts.searchCalls)
	}
	if ts.deleteCalls != 1 {
		t.Errorf("expected the delete to proceed despite the search failure, got %d", ts.deleteCalls)
	}
}

// TestDeleteStructuredProperty_DefinitionGoneSkipsBarrier verifies that a
// property whose definition cannot be read (already deleted out-of-band) is
// deleted without any settle polling.
func TestDeleteStructuredProperty_DefinitionGoneSkipsBarrier(t *testing.T) {
	shortenSettleBudget(t, 5*time.Second)

	ts := &settleTestServer{
		definition:   nil, // 404 on the definition read
		searchTotals: []int{0},
	}
	srv := httptest.NewServer(ts.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.DeleteStructuredProperty(context.Background(), "urn:li:structuredProperty:already-gone"); err != nil {
		t.Fatalf("DeleteStructuredProperty: %v", err)
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.searchCalls != 0 {
		t.Errorf("expected no settle polls when the definition is gone, got %d", ts.searchCalls)
	}
	if ts.deleteCalls != 1 {
		t.Errorf("expected exactly one delete, got %d", ts.deleteCalls)
	}
}
