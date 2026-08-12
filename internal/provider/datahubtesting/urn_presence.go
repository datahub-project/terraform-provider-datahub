// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"context"
	"fmt"
	"testing"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// Exported URN presence assertions, for callers outside this package.
//
// The acceptance suite reaches the same machinery through CheckDestroy, which
// works from a typed getter and then classifies with the unexported
// stillExistsAfterDestroyError. The live example harness
// (internal/provider/example_live_test.go) has no typed getter to work from: it
// harvests URNs out of terraform state and knows nothing about which resource
// type each one belongs to beyond its own label. What it needs is the raw
// question -- does the server hold a document for this URN -- with the husk
// classification attached to the failure. Hence these two.
//
// Both report through t.Errorf rather than t.Fatalf on purpose. A harness
// checking twenty URNs after a destroy should name all of the survivors in one
// run, not stop at the first; the operator sweeping debris wants the whole list.

// URNPresent reports whether the OpenAPI v3 entity endpoint holds a document
// carrying at least one aspect for urn.
//
// The distinction between "no document" (HTTP 404) and "a document with no
// aspects" does not matter to a caller asking whether an entity exists, and
// probeAspectShape already collapses the two. A caller that needs to tell them
// apart wants the classification in the assertions below.
func URNPresent(ctx context.Context, client *datahub.Client, urn string) (bool, error) {
	shape, err := probeAspectShape(ctx, client, urn)
	if err != nil {
		return false, err
	}
	return shape.found, nil
}

// AssertURNAbsent fails when the server still holds an entity for urn, and says
// which kind of failure it is: a CAT-2583 resurrected husk, or a genuine delete
// failure that left content aspects in place.
//
// That classification is the whole reason this is not a bare 404 check. The
// comment on the diagnostic above records weeks lost to an unclassified nightly
// flake; a failure that names the shape is a five-minute triage instead.
//
// A husk is never tolerated. It is invisible in the DataHub UI, which makes it
// tempting to treat as noise, but it still blocks re-creation of the same URN --
// so suppressing it here would hide the upstream bug this exists to surface.
func AssertURNAbsent(t *testing.T, client *datahub.Client, resourceType, urn string) {
	t.Helper()

	shape, probeErr := probeAspectShape(context.Background(), client, urn)
	if probeErr != nil {
		t.Error(describeProbeFailure("absence", resourceType, urn, probeErr))
		return
	}
	if !shape.found {
		return
	}
	t.Error(describeStillExists(resourceType, urn, shape, nil))
}

// describeProbeFailure renders the message for a probe that could not answer the
// question at all, so neither presence nor absence was established.
//
// Pure, and separated from its caller for the same reason describeStillExists is:
// a message reachable only by failing a test is a message no test can read. What
// it has to convey is that the result is UNDETERMINED rather than negative --
// silence here would otherwise be indistinguishable from a clean destroy.
func describeProbeFailure(question, resourceType, urn string, probeErr error) string {
	return fmt.Sprintf("%s %q: aspect-shape probe failed, so %s could not be established: %v. "+
		"This is not a negative result -- treating it as one would let a failed delete or an "+
		"unwritten entity report as success.", resourceType, urn, question, probeErr)
}

// describeMissingAfterApply renders the after-apply failure. Separated from
// AssertURNPresent so the wording is testable; the explanation of why `plan`
// cannot catch this is the part worth protecting from a well-meaning trim.
func describeMissingAfterApply(resourceType, urn string) string {
	return fmt.Sprintf("%s %q does not exist on the server after a successful apply. Terraform "+
		"recorded it in state, so either Create never wrote it, or it wrote a different URN "+
		"than the one it stored. Note that `terraform plan` cannot detect this: a Read that "+
		"does not consult the server still agrees with state and still plans clean.",
		resourceType, urn)
}

// AssertURNPresent fails when the server holds no entity for urn.
//
// This is the after-apply half, and it is not redundant with asserting that
// `terraform plan` is empty. A plan is clean whenever the provider's Read agrees
// with state, so a Read that returned its prior state without consulting the
// server would produce a clean plan over a server holding nothing at all. No
// amount of planning can see that; only asking the server can.
func AssertURNPresent(t *testing.T, client *datahub.Client, resourceType, urn string) {
	t.Helper()

	shape, probeErr := probeAspectShape(context.Background(), client, urn)
	if probeErr != nil {
		t.Error(describeProbeFailure("presence", resourceType, urn, probeErr))
		return
	}
	if shape.found {
		return
	}
	t.Error(describeMissingAfterApply(resourceType, urn))
}
