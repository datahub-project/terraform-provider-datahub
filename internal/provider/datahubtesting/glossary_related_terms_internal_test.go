// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// These tests pin the mock's removeRelatedTerms failure modes to the real
// RemoveRelatedTermsResolver's behaviour: it errors when the source term has
// no glossaryRelatedTerms aspect at all, and separately when the requested
// type's list within it is unset.
//
// The provider's read-before-remove guard exists solely because of these two
// errors, and it works -- which means no acceptance test ever reaches them.
// They are armed traps: the only thing that fires them is a provider that
// drops the guard. If the mock silently went lenient here (returning success
// like most aspect deletes do), the guard could be deleted with the whole
// suite staying green, and the breakage would surface only as live
// terraform destroy failures after an out-of-band edge removal. Pinning the
// error responses keeps the traps armed.

const (
	mockRelSourceURN = "urn:li:glossaryTerm:net-revenue"
	mockRelTargetURN = "urn:li:glossaryTerm:revenue"
)

// removeRelatedTermsResult invokes the mock's removeRelatedTerms handler
// directly for the mockRelSourceURN -> mockRelTargetURN edge and returns the
// GraphQL-level error message ("" on success).
func removeRelatedTermsResult(t *testing.T, s *mockServer, relationshipType string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleRemoveRelatedTerms(rec, map[string]any{
		"input": map[string]any{
			"urn":              mockRelSourceURN,
			"termUrns":         []any{mockRelTargetURN},
			"relationshipType": relationshipType,
		},
	})
	var resp struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding mock response: %v", err)
	}
	if len(resp.Errors) == 0 {
		return ""
	}
	return resp.Errors[0].Message
}

func TestMockRemoveRelatedTermsErrorsWithoutAspect(t *testing.T) {
	// Term exists but neither list was ever written: no aspect on the server.
	s := &mockServer{glossaryTerms: map[string]mockGlossaryTerm{
		"net-revenue": {URN: mockRelSourceURN, ID: "net-revenue", Name: "Net Revenue"},
	}}

	msg := removeRelatedTermsResult(t, s, "isA")
	if msg == "" {
		t.Fatal("mock accepted removeRelatedTerms on a term with no glossaryRelatedTerms aspect; the real resolver errors, and this error is what the provider's read-before-remove guard exists for")
	}
	if !strings.Contains(msg, "do not exist") {
		t.Errorf("unexpected error message: %q", msg)
	}
}

func TestMockRemoveRelatedTermsErrorsWhenTypeListUnset(t *testing.T) {
	// The aspect exists (isA was written) but the hasA list was never set.
	s := &mockServer{glossaryTerms: map[string]mockGlossaryTerm{
		"net-revenue": {
			URN:            mockRelSourceURN,
			ID:             "net-revenue",
			Name:           "Net Revenue",
			IsRelatedTerms: []string{mockRelTargetURN},
		},
	}}

	msg := removeRelatedTermsResult(t, s, "hasA")
	if msg == "" {
		t.Fatal("mock accepted removeRelatedTerms for a relationship type whose list was never written; the real resolver errors")
	}
	if !strings.Contains(msg, "do not exist for this Glossary Term") {
		t.Errorf("unexpected error message: %q", msg)
	}

	// The sibling isA edge must still be removable: proves the error above is
	// the unset-list trap, not a blanket failure.
	if msg := removeRelatedTermsResult(t, s, "isA"); msg != "" {
		t.Fatalf("removing an existing isA edge failed: %q", msg)
	}
	// An emptied list stays non-nil (distinct from never-written): a second
	// isA remove filters an empty list and succeeds, matching the resolver.
	if msg := removeRelatedTermsResult(t, s, "isA"); msg != "" {
		t.Fatalf("re-removing from an emptied (non-nil) isA list failed: %q; the mock is treating an emptied list as never-written", msg)
	}
}
