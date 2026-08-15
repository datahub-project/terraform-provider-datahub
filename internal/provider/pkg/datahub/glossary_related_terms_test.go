// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	rtSourceURN  = "urn:li:glossaryTerm:net-revenue"
	rtRelatedURN = "urn:li:glossaryTerm:revenue"
)

// relatedTermsMockServer serves one glossary term whose glossaryRelatedTerms
// aspect can be read via the OpenAPI v3 entity path, and records the GraphQL
// mutations it receives. isA/hasA hold the aspect lists (nil = list never
// written, matching the real aspect).
//
// Failure knobs: mutationErr makes every mutation return a GraphQL-level
// error; entityStatus/entityBody override the entity GET response (0 = serve
// normally). All state is guarded by mu so concurrent client calls (the lock
// serialization test) are race-free.
type relatedTermsMockServer struct {
	mu           sync.Mutex
	isA, hasA    []string
	mutations    []string // mutation names in call order
	requests     int      // total HTTP requests served
	mutationErr  string
	entityStatus int
	entityBody   string
}

// mutationNames returns a copy of the recorded mutation names.
func (m *relatedTermsMockServer) mutationNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.mutations...)
}

// requestCount returns the number of HTTP requests the mock has served.
func (m *relatedTermsMockServer) requestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requests
}

func (m *relatedTermsMockServer) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.requests++
		switch {
		case r.URL.Path == "/api/graphql":
			var req struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decoding GraphQL request: %v", err)
			}
			input, _ := req.Variables["input"].(map[string]any)
			relType, _ := input["relationshipType"].(string)
			rawTerms, _ := input["termUrns"].([]any)
			target, _ := rawTerms[0].(string)

			if m.mutationErr != "" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"errors": []map[string]any{{"message": m.mutationErr}},
				})
				return
			}

			switch {
			case strings.Contains(req.Query, "addRelatedTerms"):
				m.mutations = append(m.mutations, "addRelatedTerms")
				if relType == "isA" {
					m.isA = append(m.isA, target)
				} else {
					m.hasA = append(m.hasA, target)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"addRelatedTerms": true}})
			case strings.Contains(req.Query, "removeRelatedTerms"):
				m.mutations = append(m.mutations, "removeRelatedTerms")
				list := &m.isA
				if relType == "hasA" {
					list = &m.hasA
				}
				if *list == nil {
					// Mirror the real resolver: removing from a never-written
					// list is an error, which is why the client must read first.
					_ = json.NewEncoder(w).Encode(map[string]any{
						"errors": []map[string]any{{"message": "Failed to remove from GlossaryRelatedTerms as they do not exist for this Glossary Term"}},
					})
					return
				}
				filtered := (*list)[:0:0]
				for _, u := range *list {
					if u != target {
						filtered = append(filtered, u)
					}
				}
				if filtered == nil {
					filtered = []string{}
				}
				*list = filtered
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"removeRelatedTerms": true}})
			default:
				t.Errorf("unexpected GraphQL query: %s", req.Query)
			}
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/glossaryterm/") && m.entityStatus != 0:
			w.WriteHeader(m.entityStatus)
			_, _ = w.Write([]byte(m.entityBody))
		case r.Method == http.MethodGet && r.URL.Path == "/openapi/v3/entity/glossaryterm/"+rtSourceURN:
			value := map[string]any{}
			if m.isA != nil {
				value["isRelatedTerms"] = m.isA
			}
			if m.hasA != nil {
				value["hasRelatedTerms"] = m.hasA
			}
			resp := map[string]any{"urn": rtSourceURN}
			if len(value) > 0 {
				resp["glossaryRelatedTerms"] = map[string]any{"value": value}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func newRelatedTermsClient(t *testing.T, mock *relatedTermsMockServer) *Client {
	t.Helper()
	server := httptest.NewServer(mock.handler(t))
	t.Cleanup(server.Close)
	c, err := NewClient(server.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestAddRelatedTerm(t *testing.T) {
	ctx := context.Background()

	t.Run("adds_edge_and_reads_back", func(t *testing.T) {
		mock := &relatedTermsMockServer{}
		c := newRelatedTermsClient(t, mock)

		if err := c.AddRelatedTerm(ctx, rtSourceURN, TermRelationshipIsA, rtRelatedURN); err != nil {
			t.Fatalf("AddRelatedTerm: %v", err)
		}
		exists, err := c.RelatedTermExists(ctx, rtSourceURN, TermRelationshipIsA, rtRelatedURN)
		if err != nil {
			t.Fatalf("RelatedTermExists: %v", err)
		}
		if !exists {
			t.Fatal("edge not present after AddRelatedTerm")
		}
		// The hasA list was never written and must read back as absent.
		exists, err = c.RelatedTermExists(ctx, rtSourceURN, TermRelationshipHasA, rtRelatedURN)
		if err != nil {
			t.Fatalf("RelatedTermExists (hasA): %v", err)
		}
		if exists {
			t.Fatal("hasA edge reported present; only isA was added")
		}
	})

	t.Run("rejects_bad_input_locally", func(t *testing.T) {
		mock := &relatedTermsMockServer{}
		c := newRelatedTermsClient(t, mock)

		cases := []struct{ term, relType, related string }{
			{rtSourceURN, "contains", rtRelatedURN},               // not a TermRelationshipType
			{rtSourceURN, TermRelationshipIsA, rtSourceURN},       // self-relationship
			{"urn:li:tag:pii", TermRelationshipIsA, rtRelatedURN}, // source not a term
			{rtSourceURN, TermRelationshipIsA, "urn:li:tag:pii"},  // target not a term
			{"", TermRelationshipIsA, rtRelatedURN},               // missing source
		}
		for _, tc := range cases {
			if err := c.AddRelatedTerm(ctx, tc.term, tc.relType, tc.related); err == nil {
				t.Errorf("AddRelatedTerm(%q, %q, %q): expected error, got nil", tc.term, tc.relType, tc.related)
			}
		}
		if got := mock.mutationNames(); len(got) != 0 {
			t.Errorf("invalid input reached the server: %v", got)
		}
	})

	t.Run("surfaces_graphql_error", func(t *testing.T) {
		// The server rejects related-terms writes referentially (e.g. the
		// related term does not exist). A client that ignores the errors
		// array would report success for an edge that was never written.
		mock := &relatedTermsMockServer{mutationErr: "Failed to update " + rtSourceURN + ". urn:li:glossaryTerm:missing does not exist."}
		c := newRelatedTermsClient(t, mock)

		err := c.AddRelatedTerm(ctx, rtSourceURN, TermRelationshipIsA, rtRelatedURN)
		if err == nil {
			t.Fatal("AddRelatedTerm: expected error from GraphQL errors array, got nil")
		}
		if !strings.Contains(err.Error(), "does not exist") {
			t.Errorf("error does not carry the server message: %v", err)
		}
	})
}

func TestRemoveRelatedTerm(t *testing.T) {
	ctx := context.Background()

	t.Run("removes_existing_edge", func(t *testing.T) {
		mock := &relatedTermsMockServer{isA: []string{rtRelatedURN}}
		c := newRelatedTermsClient(t, mock)

		if err := c.RemoveRelatedTerm(ctx, rtSourceURN, TermRelationshipIsA, rtRelatedURN); err != nil {
			t.Fatalf("RemoveRelatedTerm: %v", err)
		}
		exists, err := c.RelatedTermExists(ctx, rtSourceURN, TermRelationshipIsA, rtRelatedURN)
		if err != nil {
			t.Fatalf("RelatedTermExists: %v", err)
		}
		if exists {
			t.Fatal("edge still present after RemoveRelatedTerm")
		}
	})

	t.Run("idempotent_when_edge_absent", func(t *testing.T) {
		// The list was never written: the real server errors on remove, so the
		// client must detect absence via the read path and skip the mutation.
		mock := &relatedTermsMockServer{}
		c := newRelatedTermsClient(t, mock)

		if err := c.RemoveRelatedTerm(ctx, rtSourceURN, TermRelationshipIsA, rtRelatedURN); err != nil {
			t.Fatalf("RemoveRelatedTerm on absent edge: %v", err)
		}
		for _, m := range mock.mutationNames() {
			if m == "removeRelatedTerms" {
				t.Fatal("removeRelatedTerms was called for an absent edge; the server errors on that")
			}
		}
	})

	t.Run("idempotent_when_term_gone", func(t *testing.T) {
		// GetRelatedTerms 404s (term deleted out-of-band): treated as removed.
		mock := &relatedTermsMockServer{}
		c := newRelatedTermsClient(t, mock)

		const goneURN = "urn:li:glossaryTerm:deleted-out-of-band"
		if err := c.RemoveRelatedTerm(ctx, goneURN, TermRelationshipIsA, rtRelatedURN); err != nil {
			t.Fatalf("RemoveRelatedTerm on missing term: %v", err)
		}
		if got := mock.mutationNames(); len(got) != 0 {
			t.Errorf("mutation fired for a missing term: %v", got)
		}
	})

	t.Run("fails_when_read_fails", func(t *testing.T) {
		// The pre-remove read exists to answer "is the edge still there?".
		// When that read errors, the only safe answer is to fail the delete:
		// treating a failed read as "already gone" would let terraform
		// destroy report success while the edge stays in DataHub.
		mock := &relatedTermsMockServer{isA: []string{rtRelatedURN}, entityStatus: http.StatusBadGateway, entityBody: "upstream unavailable"}
		c := newRelatedTermsClient(t, mock)

		if err := c.RemoveRelatedTerm(ctx, rtSourceURN, TermRelationshipIsA, rtRelatedURN); err == nil {
			t.Fatal("RemoveRelatedTerm: expected error when the existence read fails, got nil")
		}
		if got := mock.mutationNames(); len(got) != 0 {
			t.Errorf("mutation fired despite the failed read: %v", got)
		}
	})

	t.Run("surfaces_graphql_error", func(t *testing.T) {
		// Edge present, so the mutation fires -- and its rejection must
		// propagate. A client that drops the errors array would let destroy
		// succeed while the edge survives on the server.
		mock := &relatedTermsMockServer{isA: []string{rtRelatedURN}, mutationErr: "Unauthorized to perform this action."}
		c := newRelatedTermsClient(t, mock)

		err := c.RemoveRelatedTerm(ctx, rtSourceURN, TermRelationshipIsA, rtRelatedURN)
		if err == nil {
			t.Fatal("RemoveRelatedTerm: expected error from GraphQL errors array, got nil")
		}
		if !strings.Contains(err.Error(), "Unauthorized") {
			t.Errorf("error does not carry the server message: %v", err)
		}
	})
}

func TestGetRelatedTerms(t *testing.T) {
	ctx := context.Background()

	t.Run("nil_for_missing_term", func(t *testing.T) {
		mock := &relatedTermsMockServer{}
		c := newRelatedTermsClient(t, mock)

		rel, err := c.GetRelatedTerms(ctx, "urn:li:glossaryTerm:missing")
		if err != nil {
			t.Fatalf("GetRelatedTerms: %v", err)
		}
		if rel != nil {
			t.Fatalf("expected nil for a missing term, got %+v", rel)
		}
	})

	t.Run("empty_for_term_without_aspect", func(t *testing.T) {
		mock := &relatedTermsMockServer{}
		c := newRelatedTermsClient(t, mock)

		rel, err := c.GetRelatedTerms(ctx, rtSourceURN)
		if err != nil {
			t.Fatalf("GetRelatedTerms: %v", err)
		}
		if rel == nil {
			t.Fatal("expected non-nil RelatedTerms for an existing term")
		}
		if len(rel.IsA) != 0 || len(rel.HasA) != 0 {
			t.Fatalf("expected empty lists, got %+v", rel)
		}
	})

	t.Run("parses_both_lists", func(t *testing.T) {
		mock := &relatedTermsMockServer{
			isA:  []string{rtRelatedURN},
			hasA: []string{"urn:li:glossaryTerm:component"},
		}
		c := newRelatedTermsClient(t, mock)

		rel, err := c.GetRelatedTerms(ctx, rtSourceURN)
		if err != nil {
			t.Fatalf("GetRelatedTerms: %v", err)
		}
		if !rel.Has(TermRelationshipIsA, rtRelatedURN) {
			t.Error("isA edge missing from parse")
		}
		if !rel.Has(TermRelationshipHasA, "urn:li:glossaryTerm:component") {
			t.Error("hasA edge missing from parse")
		}
		if rel.Has(TermRelationshipIsA, "urn:li:glossaryTerm:component") {
			t.Error("hasA target leaked into the isA list")
		}
	})

	t.Run("rejects_non_term_urn", func(t *testing.T) {
		mock := &relatedTermsMockServer{}
		c := newRelatedTermsClient(t, mock)

		if _, err := c.GetRelatedTerms(ctx, "urn:li:tag:pii"); err == nil {
			t.Fatal("expected error for a non-glossaryTerm URN")
		}
	})

	t.Run("surfaces_http_error", func(t *testing.T) {
		// A non-404 failure must be an error, never "no relationships": the
		// read backs both drift detection and the idempotent-delete guard, and
		// either one acting on a misread 5xx would silently drop or orphan the
		// edge. The message must carry the status and body for diagnosis.
		mock := &relatedTermsMockServer{entityStatus: http.StatusInternalServerError, entityBody: "java.lang.RuntimeException: boom"}
		c := newRelatedTermsClient(t, mock)

		_, err := c.GetRelatedTerms(ctx, rtSourceURN)
		if err == nil {
			t.Fatal("GetRelatedTerms: expected error for HTTP 500, got nil")
		}
		if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "boom") {
			t.Errorf("error missing status or server body: %v", err)
		}

		// RelatedTermExists must propagate the same failure, not report false.
		if _, err := c.RelatedTermExists(ctx, rtSourceURN, TermRelationshipIsA, rtRelatedURN); err == nil {
			t.Fatal("RelatedTermExists: expected error for HTTP 500, got nil")
		}
	})

	t.Run("errors_on_malformed_response", func(t *testing.T) {
		// A 200 whose body does not parse must be an error. Decoding into an
		// empty struct instead would read as "no relationships", and the next
		// plan would silently destroy and re-create every edge on the term.
		mock := &relatedTermsMockServer{entityStatus: http.StatusOK, entityBody: "<html>gateway timeout</html>"}
		c := newRelatedTermsClient(t, mock)

		_, err := c.GetRelatedTerms(ctx, rtSourceURN)
		if err == nil {
			t.Fatal("GetRelatedTerms: expected error for a non-JSON body, got nil")
		}
		if !strings.Contains(err.Error(), "parsing glossaryRelatedTerms response") {
			t.Errorf("unexpected error for malformed body: %v", err)
		}
	})
}

// TestRelatedTermWrites_HoldPerTermLock proves AddRelatedTerm and
// RemoveRelatedTerm actually take the per-source-term lock before touching the
// server. TestKeyedMutex_* prove the primitive; this proves the wiring -- it
// fails if either method stops calling lockTermRelatedTerms or keys the lock
// on anything but the source term URN, which would reintroduce the CAT-2568
// lost-edge race between parallel Terraform resource operations.
//
// The check is deterministic in the failing direction: with the lock held by
// the test, a correctly-wired call blocks before its first HTTP request, so
// zero requests is a hard invariant; an unwired call completes in
// microseconds, far inside the wait window.
//
// What this cannot prove: that holding the lock is sufficient to prevent the
// server-side read-modify-write losing edges. That failure lives in the real
// resolver's non-atomicity and is not reproducible against a mock that
// applies mutations atomically.
func TestRelatedTermWrites_HoldPerTermLock(t *testing.T) {
	ctx := context.Background()

	ops := []struct {
		name string
		call func(c *Client) error
	}{
		{"AddRelatedTerm", func(c *Client) error {
			return c.AddRelatedTerm(ctx, rtSourceURN, TermRelationshipIsA, rtRelatedURN)
		}},
		{"RemoveRelatedTerm", func(c *Client) error {
			return c.RemoveRelatedTerm(ctx, rtSourceURN, TermRelationshipIsA, rtRelatedURN)
		}},
	}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			mock := &relatedTermsMockServer{isA: []string{rtRelatedURN}}
			c := newRelatedTermsClient(t, mock)

			unlock := c.lockTermRelatedTerms(rtSourceURN)
			done := make(chan error, 1)
			go func() { done <- op.call(c) }()

			select {
			case err := <-done:
				t.Fatalf("%s completed while the per-term lock was held (err=%v); the write is not serialized per source term", op.name, err)
			case <-time.After(150 * time.Millisecond):
				// Still blocked on the lock, as required.
			}
			if got := mock.requestCount(); got != 0 {
				t.Fatalf("%d HTTP request(s) reached the server while the per-term lock was held", got)
			}

			unlock()
			if err := <-done; err != nil {
				t.Fatalf("%s after unlock: %v", op.name, err)
			}
		})
	}
}
