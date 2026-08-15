// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	rtSourceURN  = "urn:li:glossaryTerm:net-revenue"
	rtRelatedURN = "urn:li:glossaryTerm:revenue"
)

// relatedTermsMockServer serves one glossary term whose glossaryRelatedTerms
// aspect can be read via the OpenAPI v3 entity path, and records the GraphQL
// mutations it receives. isA/hasA hold the aspect lists (nil = list never
// written, matching the real aspect).
type relatedTermsMockServer struct {
	isA, hasA []string
	mutations []string // mutation names in call order
}

func (m *relatedTermsMockServer) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
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
		if len(mock.mutations) != 0 {
			t.Errorf("invalid input reached the server: %v", mock.mutations)
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
		for _, m := range mock.mutations {
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
		if len(mock.mutations) != 0 {
			t.Errorf("mutation fired for a missing term: %v", mock.mutations)
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
}
