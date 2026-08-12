// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The contract that matters here is error-versus-absence, and it is what stops
// the live example harness's destroy assertions from passing vacuously.
//
// AssertURNAbsent and the end-of-run sweep both treat "not present" as success.
// If a probe error -- a flaky GMS 500, a proxy hiccup -- collapsed to
// present=false, then a genuine delete failure would report as a clean destroy,
// the sweep would agree, and the harness would certify an instance it had never
// successfully read. That is the one failure mode a test can pin here, so it is
// the one this file is about.
func TestURNPresent(t *testing.T) {
	t.Parallel()

	const urn = "urn:li:domain:test-presence"

	cases := []struct {
		name        string
		status      int
		body        string
		wantPresent bool
		wantErr     bool
	}{
		{
			name:        "populated entity is present",
			status:      200,
			body:        `{"urn":"` + urn + `","domainKey":{},"domainProperties":{}}`,
			wantPresent: true,
		},
		{
			// A husk is present. It carries no content aspect and is invisible in
			// the DataHub UI, but it still blocks re-creation of its URN, so
			// reporting it as absent would defeat the whole diagnostic.
			name:        "husk is present, not absent",
			status:      200,
			body:        `{"urn":"` + urn + `","domainKey":{},"structuredProperties":{}}`,
			wantPresent: true,
		},
		{
			name:        "404 is absent",
			status:      404,
			body:        `{}`,
			wantPresent: false,
		},
		{
			// A document with an envelope and no aspects is absence, matching
			// probeAspectShape's own collapse of the two cases. A caller needing
			// to tell them apart wants the classified assertions instead.
			name:        "aspectless document is absent",
			status:      200,
			body:        `{"urn":"` + urn + `"}`,
			wantPresent: false,
		},
		{
			// The case with teeth: a server error must surface as an error, never
			// as absence. present=false here would be indistinguishable from a
			// successful delete.
			name:    "server error is an error, not absence",
			status:  500,
			body:    `{"error":"boom"}`,
			wantErr: true,
		},
		{
			name:    "unparseable body is an error, not absence",
			status:  200,
			body:    `not json`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := entityDocServer(t, tc.status, tc.body)
			present, err := URNPresent(context.Background(), client, urn)

			switch {
			case tc.wantErr && err == nil:
				t.Fatalf("URNPresent returned no error, want one; present=%v. An error "+
					"collapsing to present=false would let a failed delete read as a "+
					"successful one.", present)
			case !tc.wantErr && err != nil:
				t.Fatalf("URNPresent returned unexpected error: %v", err)
			}
			if tc.wantErr {
				// present is not meaningful alongside an error, and callers must
				// not consult it; nothing to assert.
				return
			}
			if present != tc.wantPresent {
				t.Errorf("URNPresent = %v, want %v", present, tc.wantPresent)
			}
		})
	}
}

// The two assertions must stay SILENT when the server agrees with them. Only
// the passing direction is testable: both report failure through t.Errorf on a
// concrete *testing.T, so exercising a failure branch would fail this test. That
// is not a gap worth reshaping the signatures for -- the messages themselves are
// covered by TestDescribeStillExists, and a false positive is the hazard that
// actually costs something, since it would fail every harness run against a
// correctly-behaving instance.
func TestAssertionsPassWhenServerAgrees(t *testing.T) {
	t.Parallel()

	const urn = "urn:li:domain:test-agrees"

	t.Run("AssertURNAbsent is silent on 404", func(t *testing.T) {
		t.Parallel()
		AssertURNAbsent(t, entityDocServer(t, 404, `{}`), "datahub_domain", urn)
	})

	t.Run("AssertURNAbsent is silent on an aspectless document", func(t *testing.T) {
		t.Parallel()
		AssertURNAbsent(t, entityDocServer(t, 200, `{"urn":"`+urn+`"}`), "datahub_domain", urn)
	})

	t.Run("AssertURNPresent is silent on a populated entity", func(t *testing.T) {
		t.Parallel()
		body := `{"urn":"` + urn + `","domainKey":{},"domainProperties":{}}`
		AssertURNPresent(t, entityDocServer(t, 200, body), "datahub_domain", urn)
	})

	t.Run("AssertURNPresent is silent on a husk", func(t *testing.T) {
		t.Parallel()
		// A husk satisfies presence. Whether that is the right thing to assert is
		// the caller's business: home-page-layout's mustSurvive check wants the
		// adopted template to exist, and a husk there would be a separate defect
		// caught by the harness's own re-apply and sweep, not by this function.
		body := `{"urn":"` + urn + `","domainKey":{},"structuredProperties":{}}`
		AssertURNPresent(t, entityDocServer(t, 200, body), "datahub_domain", urn)
	})
}

// The two message builders are pure so that the wording is readable by a test
// rather than only by whoever trips the failure. What is asserted is the load
// bearing content, not the prose: substrings, in the style of
// TestDescribeStillExists next door.
func TestDescribeProbeFailure(t *testing.T) {
	t.Parallel()

	msg := describeProbeFailure("absence", "datahub_domain",
		"urn:li:domain:x", errors.New("connection refused"))

	for _, want := range []string{
		"datahub_domain",
		`"urn:li:domain:x"`,
		"absence could not be established",
		"connection refused",
		// The point of the message: undetermined, not negative.
		"not a negative result",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("describeProbeFailure() missing %q\ngot: %s", want, msg)
		}
	}

	// The question is a parameter because the same failure means different
	// things to the two callers, and a message naming the wrong one would point
	// a maintainer at the opposite problem.
	if presence := describeProbeFailure("presence", "datahub_tag", "urn:li:tag:y", errors.New("boom")); !strings.Contains(presence, "presence could not be established") {
		t.Errorf("describeProbeFailure() did not carry the caller's question\ngot: %s", presence)
	}
}

func TestDescribeMissingAfterApply(t *testing.T) {
	t.Parallel()

	msg := describeMissingAfterApply("datahub_tag", "urn:li:tag:tf-example-pii")

	for _, want := range []string{
		"datahub_tag",
		`"urn:li:tag:tf-example-pii"`,
		"does not exist on the server after a successful apply",
		// Both candidate causes, so the reader is not left to guess.
		"Create never wrote it",
		"different URN",
		// The reason this assertion exists at all, and the sentence most at risk
		// of being trimmed by someone who thinks plan already covers it.
		"terraform plan` cannot detect this",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("describeMissingAfterApply() missing %q\ngot: %s", want, msg)
		}
	}
}

// URNPresent must reject a URN it cannot turn into an entity path rather than
// reporting absence. The harness derives URNs from terraform state, so a
// resource exposing something other than a URN in its urn attribute lands here
// -- and "absent" would be exactly the wrong answer, since it reads as a
// successful destroy.
func TestURNPresentRejectsMalformedURN(t *testing.T) {
	t.Parallel()

	client := entityDocServer(t, 200, `{}`)

	for _, urn := range []string{
		"",
		"not-a-urn",
		"urn:li:domain",
		"A description that happens to sit in the wrong attribute",
	} {
		present, err := URNPresent(context.Background(), client, urn)
		if err == nil {
			t.Errorf("URNPresent(%q) returned no error (present=%v); a URN that cannot be "+
				"parsed must not be reported as absent", urn, present)
		}
	}
}
