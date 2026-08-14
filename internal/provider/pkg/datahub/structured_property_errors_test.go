// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"strings"
	"testing"
)

// The contract worth pinning is not the wording of any hint -- it is that the
// server's own text always survives. Matching is on strings DataHub controls, so
// a reworded message must degrade to today's behaviour (raw body, no help)
// rather than swallow the real error.

// A realistic rejection body, trimmed from an actual HTTP 400 observed against
// Quickstart v1.7.0 while re-creating a property whose id had been used.
const collisionBody = `{"error":"Validation Error","message":"Failed to validate MCP due to: ` +
	`ValidationExceptionCollection{EntityAspect:(urn:li:structuredProperty:tf-example.governance.tier,propertyDefinition) ` +
	`Exceptions: [AspectValidationException(subType=VALIDATION, msg=Structured property Elasticsearch field ` +
	`'tf-example_governance_tier' collides with existing property mapping. Qualified names that differ only by ` +
	`'.' vs '_' normalize to the same field name (proposed qualifiedName='tf-example.governance.tier').)]}"}`

func TestExplainStructuredPropertyRejectionPreservesBody(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{name: "recognised rejection", body: collisionBody},
		{name: "unrecognised rejection", body: `{"error":"Validation Error","message":"something new upstream"}`},
		{name: "empty body", body: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := explainStructuredPropertyRejection(tc.body)
			if !strings.HasPrefix(got, tc.body) {
				t.Errorf("server text did not survive. The original must always be readable, "+
					"because the match is on wording DataHub controls.\nbody: %q\ngot:  %q", tc.body, got)
			}
		})
	}
}

func TestExplainStructuredPropertyRejectionUnknownIsUnchanged(t *testing.T) {
	t.Parallel()

	// The degradation path: a rejection we have not catalogued, or one whose
	// wording moved, must come back byte-identical rather than gaining a hint
	// that does not apply.
	body := `msg=Some validator that did not exist when this was written`
	if got := explainStructuredPropertyRejection(body); got != body {
		t.Errorf("unrecognised rejection was modified\nwant: %q\ngot:  %q", body, got)
	}
}

func TestExplainStructuredPropertyRejectionCollision(t *testing.T) {
	t.Parallel()

	got := explainStructuredPropertyRejection(collisionBody)

	for _, want := range []string{
		"used before on this instance",
		"deleting",  // ... does NOT release that field
		"`version`", // the actual fix
		"14-digit",
		"18974",
		// The server's own explanation is misleading, so the hint must say so.
		// This is the sentence that cost two wrong diagnoses.
		"Disregard",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("collision hint missing %q\ngot: %s", want, got)
		}
	}
}

func TestExplainStructuredPropertyRejectionMultiple(t *testing.T) {
	t.Parallel()

	// A ValidationExceptionCollection is a list, so more than one validator can
	// fail in a single write. Reporting only the first would send the reader
	// round the loop again for the second.
	body := `Exceptions: [msg=Invalid version specified. Must match [0-9]{14}, ` +
		`msg=Value type cannot be changed as this is a backwards incompatible change]`

	got := explainStructuredPropertyRejection(body)
	if !strings.Contains(got, "2 validation failures") {
		t.Errorf("multiple failures were not announced as such\ngot: %s", got)
	}
	if !strings.Contains(got, "14 digits") {
		t.Errorf("version hint missing from a multi-failure body\ngot: %s", got)
	}
	if !strings.Contains(got, "breaking change") {
		t.Errorf("value-type hint missing from a multi-failure body\ngot: %s", got)
	}
}

func TestExplainStructuredPropertyRejectionCaseInsensitive(t *testing.T) {
	t.Parallel()

	// Matching is case-insensitive so a capitalisation change upstream does not
	// silently drop the hint.
	if got := explainStructuredPropertyRejection("MSG=COLLIDES WITH EXISTING PROPERTY MAPPING"); !strings.Contains(got, "`version`") {
		t.Errorf("hint was dropped by a case change\ngot: %s", got)
	}
}

// Every hint must be substantial enough to act on, in the spirit of the
// reason-length floors elsewhere in this repository. A hint that merely restates
// the server's message costs the reader a second read for nothing.
func TestStructuredPropertyHintsAreSubstantial(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool, len(structuredPropertyHints))
	for _, h := range structuredPropertyHints {
		if h.match == "" {
			t.Error("a hint has an empty match, which would fire on every rejection")
		}
		if seen[h.match] {
			t.Errorf("duplicate match %q would append the same hint twice", h.match)
		}
		seen[h.match] = true

		if len(h.hint) < 60 {
			t.Errorf("hint for %q is %d chars, too short to tell the reader what to do: %q",
				h.match, len(h.hint), h.hint)
		}
	}
}
