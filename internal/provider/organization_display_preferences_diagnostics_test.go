// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

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

	t.Run("unsupported_primary_color_names_the_attribute_and_keeps_the_server_message", func(t *testing.T) {
		t.Parallel()
		// The user who configures a brand colour against a DataHub Cloud
		// instance too old to have the field. The diagnostic deliberately does
		// NOT name a DataHub version: the provider holds no per-attribute table
		// of backend versions, because DataHub's three numbering schemes have no
		// total order and any such table rots. It names the Terraform attribute
		// (what the practitioner edits) and points at the provider CHANGELOG
		// (which does record what each attribute needs). Keeping the server's
		// own text is what makes the deliberately loose detector safe to
		// misfire.
		wrapped := fmt.Errorf("writing organization display preferences: %w",
			&datahub.UnsupportedFieldError{
				Attribute: "primary_color",
				Field:     "primaryColor",
				Message:   "Field 'primaryColor' is not defined",
			})
		summary, detail := capture(wrapped)

		if !strings.Contains(summary, "primary_color") {
			t.Errorf("summary = %q, want it to name the attribute", summary)
		}
		if !strings.Contains(detail, "primary_color") {
			t.Errorf("detail = %q, want it to name the attribute to remove", detail)
		}
		if !strings.Contains(detail, "CHANGELOG") {
			t.Errorf("detail = %q, want it to point at the changelog", detail)
		}
		if !strings.Contains(detail, "Field 'primaryColor' is not defined") {
			t.Errorf("detail = %q, want the server message preserved", detail)
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

// TestPrimaryColorToWrite pins the three-way decision that makes primary_color
// safe to add to an already-shipped resource.
//
// The nil case is the important one and it is invisible from the outside: it is
// the difference between a mutation that names primaryColor and one that does
// not, and DataHub Cloud instances older than v2.2.0 reject the whole write
// over the former. Nothing in an acceptance test asserts it directly, so it is
// asserted here.
func TestPrimaryColorToWrite(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		planned types.String
		prior   types.String
		want    *string
	}{
		{
			name:    "never configured sends nothing",
			planned: types.StringNull(),
			prior:   types.StringNull(),
			want:    nil,
		},
		{
			name:    "configured value is sent",
			planned: types.StringValue("#EC0016"),
			prior:   types.StringNull(),
			want:    strPtr("#EC0016"),
		},
		{
			name:    "explicit empty string is an instruction, not an absence",
			planned: types.StringValue(""),
			prior:   types.StringValue("#EC0016"),
			want:    strPtr(""),
		},
		{
			// Without this the apply would report success, write null to
			// state, and leave the instance holding a colour nothing claims.
			name:    "removing a previously set value clears it",
			planned: types.StringNull(),
			prior:   types.StringValue("#EC0016"),
			want:    strPtr(""),
		},
		{
			// Destroy passes a null plan and whatever state held, so this is
			// the same branch as the case above.
			name:    "destroy clears a value it was managing",
			planned: types.StringNull(),
			prior:   types.StringValue(""),
			want:    strPtr(""),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := primaryColorToWrite(tc.planned, tc.prior)
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("got %q, want the field omitted from the write entirely", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("field omitted from the write, want %q sent", *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Fatalf("got %q, want %q", *got, *tc.want)
			}
		})
	}
}

// TestCanonicalPrimaryColor covers the read-side half of the same rule.
//
// An undeclared attribute must not absorb the instance's value: doing so would
// present a colour someone set in the DataHub UI as drift, and the next apply
// would clear it.
func TestCanonicalPrimaryColor(t *testing.T) {
	t.Parallel()

	t.Run("an_undeclared_attribute_never_adopts_the_server_value", func(t *testing.T) {
		t.Parallel()
		if got := canonicalPrimaryColor(types.StringNull(), "#123456"); !got.IsNull() {
			t.Errorf("got %v, want null so an unmanaged colour is left alone", got)
		}
	})

	t.Run("a_managed_attribute_surfaces_drift", func(t *testing.T) {
		t.Parallel()
		got := canonicalPrimaryColor(types.StringValue("#EC0016"), "#123456")
		if got.ValueString() != "#123456" {
			t.Errorf("got %v, want the server value so drift is visible", got)
		}
	})

	t.Run("a_managed_attribute_surfaces_a_cleared_value", func(t *testing.T) {
		t.Parallel()
		got := canonicalPrimaryColor(types.StringValue("#EC0016"), "")
		if got.IsNull() || got.ValueString() != "" {
			t.Errorf("got %v, want an empty string rather than null", got)
		}
	})
}

// TestHexColorOrEmptyValidator checks the one way this validator differs from
// the shared hexColorValidator it delegates to: "" is DataHub's clear-to-default
// sentinel, so rejecting it would make the brand colour impossible to reset.
func TestHexColorOrEmptyValidator(t *testing.T) {
	t.Parallel()

	check := func(v types.String) diag.Diagnostics {
		resp := &validator.StringResponse{}
		hexColorOrEmptyValidator{}.ValidateString(
			t.Context(),
			validator.StringRequest{Path: path.Root("primary_color"), ConfigValue: v},
			resp,
		)
		return resp.Diagnostics
	}

	accepted := []types.String{
		types.StringValue(""),
		types.StringValue("#EC0016"),
		types.StringValue("#ec0016"),
		types.StringNull(),
		types.StringUnknown(),
	}
	for _, v := range accepted {
		if diags := check(v); diags.HasError() {
			t.Errorf("value %v rejected: %v", v, diags)
		}
	}

	rejected := []types.String{
		types.StringValue("red"),
		types.StringValue("EC0016"),
		types.StringValue("#EC001"),
		types.StringValue(" "),
	}
	for _, v := range rejected {
		if diags := check(v); !diags.HasError() {
			t.Errorf("value %v accepted, want a plan-time rejection", v)
		}
	}
}

func strPtr(s string) *string { return &s }
