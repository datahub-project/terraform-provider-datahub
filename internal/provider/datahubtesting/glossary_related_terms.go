// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// relatedTermsInput extracts the RelatedTermsInput fields from the mutation
// variables: {urn, termUrns, relationshipType}.
func relatedTermsInput(variables map[string]any) (urn string, termURNs []string, relationshipType string) {
	input, _ := variables["input"].(map[string]any)
	urn, _ = input["urn"].(string)
	relationshipType, _ = input["relationshipType"].(string)
	raw, _ := input["termUrns"].([]any)
	for _, t := range raw {
		if s, ok := t.(string); ok {
			termURNs = append(termURNs, s)
		}
	}
	return urn, termURNs, relationshipType
}

// relatedTermsGraphQLError writes a GraphQL-level error, matching the real
// server's response shape for a failed related-terms mutation.
func relatedTermsGraphQLError(w http.ResponseWriter, mutationName, message string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]any{{"message": message}},
		"data":   map[string]any{mutationName: nil},
	})
}

// validateRelatedTermsSource mirrors the resolver's source-term checks: the
// URN must be a glossaryTerm and must exist. Returns the stored term's map key
// and false (with the error already written) on failure. Caller holds s.mu.
func (s *mockServer) validateRelatedTermsSource(w http.ResponseWriter, mutationName, urn string) (string, bool) {
	id := strings.TrimPrefix(urn, "urn:li:glossaryTerm:")
	_, exists := s.glossaryTerms[id]
	if !strings.HasPrefix(urn, "urn:li:glossaryTerm:") || !exists {
		relatedTermsGraphQLError(w, mutationName,
			fmt.Sprintf("Failed to update %s. %s either does not exist or is not a glossaryTerm.", urn, urn))
		return "", false
	}
	return id, true
}

// handleAddRelatedTerms implements the addRelatedTerms mutation from the
// GraphQL schema and the AddRelatedTermsResolver, not from what the provider's
// client happens to send: relationshipType must be a TermRelationshipType enum
// value, the source must be an existing glossaryTerm, and every target must be
// an existing glossaryTerm distinct from the source. The edge is appended to
// the source term's isRelatedTerms/hasRelatedTerms list, deduplicated -- the
// real resolver skips already-present targets. No inverse edge is written on
// the target term, matching the server.
func (s *mockServer) handleAddRelatedTerms(w http.ResponseWriter, variables map[string]any) {
	const mutationName = "addRelatedTerms"
	urn, termURNs, relationshipType := relatedTermsInput(variables)

	if relationshipType != "isA" && relationshipType != "hasA" {
		// The real server rejects this during enum coercion, before the resolver.
		relatedTermsGraphQLError(w, mutationName,
			fmt.Sprintf("Invalid value '%s' for enum 'TermRelationshipType'", relationshipType))
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	id, ok := s.validateRelatedTermsSource(w, mutationName, urn)
	if !ok {
		return
	}

	for _, target := range termURNs {
		switch {
		case target == urn:
			relatedTermsGraphQLError(w, mutationName,
				fmt.Sprintf("Failed to update %s. Tried to create related term with itself.", urn))
			return
		case !strings.HasPrefix(target, "urn:li:glossaryTerm:"):
			relatedTermsGraphQLError(w, mutationName,
				fmt.Sprintf("Failed to update %s. %s is not a glossaryTerm.", urn, target))
			return
		default:
			if _, exists := s.glossaryTerms[strings.TrimPrefix(target, "urn:li:glossaryTerm:")]; !exists {
				relatedTermsGraphQLError(w, mutationName,
					fmt.Sprintf("Failed to update %s. %s does not exist.", urn, target))
				return
			}
		}
	}

	t := s.glossaryTerms[id]
	for _, target := range termURNs {
		if relationshipType == "isA" {
			if !contains(t.IsRelatedTerms, target) {
				t.IsRelatedTerms = append(t.IsRelatedTerms, target)
			}
		} else {
			if !contains(t.HasRelatedTerms, target) {
				t.HasRelatedTerms = append(t.HasRelatedTerms, target)
			}
		}
	}
	s.glossaryTerms[id] = t

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{mutationName: true},
	})
}

// handleRemoveRelatedTerms implements the removeRelatedTerms mutation from the
// RemoveRelatedTermsResolver, including its failure mode when nothing is there
// to remove: the real resolver errors when the source term has no
// glossaryRelatedTerms aspect at all, and separately when the requested type's
// list is unset. Keeping those errors in the mock is what proves the provider's
// read-before-remove idempotence guard is actually needed and exercised.
func (s *mockServer) handleRemoveRelatedTerms(w http.ResponseWriter, variables map[string]any) {
	const mutationName = "removeRelatedTerms"
	urn, termURNs, relationshipType := relatedTermsInput(variables)

	if relationshipType != "isA" && relationshipType != "hasA" {
		relatedTermsGraphQLError(w, mutationName,
			fmt.Sprintf("Invalid value '%s' for enum 'TermRelationshipType'", relationshipType))
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	id, ok := s.validateRelatedTermsSource(w, mutationName, urn)
	if !ok {
		return
	}

	t := s.glossaryTerms[id]
	if t.IsRelatedTerms == nil && t.HasRelatedTerms == nil {
		relatedTermsGraphQLError(w, mutationName,
			fmt.Sprintf("Related Terms for this Urn do not exist: %s", urn))
		return
	}

	list := t.IsRelatedTerms
	if relationshipType == "hasA" {
		list = t.HasRelatedTerms
	}
	if list == nil {
		relatedTermsGraphQLError(w, mutationName,
			"Failed to remove from GlossaryRelatedTerms as they do not exist for this Glossary Term")
		return
	}

	filtered := list[:0:0]
	for _, u := range list {
		if !contains(termURNs, u) {
			filtered = append(filtered, u)
		}
	}
	// Keep an emptied list non-nil: the real aspect retains an empty array,
	// distinct from an aspect that never had the list.
	if filtered == nil {
		filtered = []string{}
	}
	if relationshipType == "isA" {
		t.IsRelatedTerms = filtered
	} else {
		t.HasRelatedTerms = filtered
	}
	s.glossaryTerms[id] = t

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{mutationName: true},
	})
}
