// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// TestOrganizationDisplayPreferencesAddWriteError covers the diagnostic mapping
// for write failures.
//
// This is the entire experience of a user who points the resource at an
// open-source instance, and no test that runs in PR CI reaches it: the
// acceptance path needs a live OSS target (nightly Quickstart only). A
// regression here would silently replace an actionable message with a raw
// GraphQL schema error.
func TestOrganizationDisplayPreferencesAddWriteError(t *testing.T) {
	t.Parallel()

	capture := func(err error) (summary, detail string) {
		r := &organizationDisplayPreferencesResource{}
		r.addWriteError(func(s, d string) { summary, detail = s, d }, err)
		return summary, detail
	}

	t.Run("cloud_only_sentinel_maps_to_actionable_diagnostic", func(t *testing.T) {
		t.Parallel()
		summary, detail := capture(datahub.ErrOrganizationDisplayPreferencesCloudOnly)

		if !strings.Contains(summary, "require DataHub Cloud") {
			t.Errorf("summary = %q, want it to mention requiring DataHub Cloud", summary)
		}
		if !strings.Contains(detail, "datahub_organization_display_preferences") {
			t.Errorf("detail = %q, want it to name the resource to remove", detail)
		}
	})

	t.Run("cloud_only_sentinel_still_maps_when_wrapped", func(t *testing.T) {
		t.Parallel()
		// apply() wraps the client error, so the mapping has to survive
		// unwrapping or the Cloud-only case silently degrades.
		wrapped := fmt.Errorf("writing organization display preferences: %w",
			datahub.ErrOrganizationDisplayPreferencesCloudOnly)
		summary, _ := capture(wrapped)

		if !strings.Contains(summary, "require DataHub Cloud") {
			t.Errorf("summary = %q, want the Cloud-only diagnostic for a wrapped sentinel", summary)
		}
	})

	t.Run("other_errors_surface_verbatim", func(t *testing.T) {
		t.Parallel()
		// A privilege denial (the realistic failure on Cloud, when the caller
		// lacks MANAGE_ORGANIZATION_DISPLAY_PREFERENCES) must not be
		// misreported as "requires DataHub Cloud".
		summary, detail := capture(errors.New("DataHub API error: Unauthorized to perform this action"))

		if summary != "DataHub API Error" {
			t.Errorf("summary = %q, want %q", summary, "DataHub API Error")
		}
		if !strings.Contains(detail, "Unauthorized") {
			t.Errorf("detail = %q, want the underlying message preserved", detail)
		}
	})
}

// TestOptionalStringValue covers the import/data-source mapping of a blank
// server value to null. DataHub cannot remove these fields, only blank them, so
// an unset preference reads back as "" and must become null - otherwise an
// imported resource shows a spurious "" -> null diff on its first plan.
func TestOptionalStringValue(t *testing.T) {
	t.Parallel()

	if got := optionalStringValue(""); !got.IsNull() {
		t.Errorf("optionalStringValue(\"\") = %v, want null", got)
	}
	if got := optionalStringValue("Acme Data"); got.ValueString() != "Acme Data" {
		t.Errorf("optionalStringValue(%q) = %v, want the value preserved", "Acme Data", got)
	}
}
