// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// nonEmptyStringValidator rejects an explicit empty string on an optional
// attribute whose "unset" spelling is omission. Accepting "" alongside null
// creates two spellings of the same intent, and the read path (which
// normalises the server's "" to null) would then produce perpetual drift for
// the ""-spelled config. Hand-rolled per the project convention of not taking
// the framework-validators dependency.
type nonEmptyStringValidator struct{}

func nonEmptyString() nonEmptyStringValidator {
	return nonEmptyStringValidator{}
}

func (v nonEmptyStringValidator) Description(_ context.Context) string {
	return "must not be an empty string; omit the attribute instead"
}

func (v nonEmptyStringValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v nonEmptyStringValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if req.ConfigValue.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Empty string not allowed",
			"This attribute must not be set to an empty string. Omit it entirely instead.",
		)
	}
}

// nonEmptyStringListValidator rejects an empty list and any null, unknown-safe
// empty-string element in a string-list attribute. An empty list is forbidden
// for the same reason nonEmptyStringMapValidator forbids an empty map: the
// provider clears the server-side list when the attribute is omitted, and an
// explicitly configured [] would read back as null and produce perpetual
// drift.
type nonEmptyStringListValidator struct{}

func (v nonEmptyStringListValidator) Description(_ context.Context) string {
	return "must be omitted or contain only non-empty strings"
}

func (v nonEmptyStringListValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v nonEmptyStringListValidator) ValidateList(_ context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	elems := req.ConfigValue.Elements()
	if len(elems) == 0 {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Empty list not allowed",
			"This attribute must not be set to an empty list. Omit it entirely to clear the server-side value.",
		)
		return
	}
	for i, elem := range elems {
		sv, ok := elem.(types.String)
		if !ok || sv.IsUnknown() {
			continue // cannot validate a value that is not yet known
		}
		if sv.IsNull() || sv.ValueString() == "" {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Empty element not allowed",
				fmt.Sprintf("Element %d is null or an empty string. Provide a non-empty string, or remove it.", i),
			)
		}
	}
}

// urnIDValidator validates a user-supplied URN id (the part after the last
// colon of a single-part URN). It mirrors the DataHub Python SDK's
// UrnEncoder.contains_reserved_char check, so an id the provider accepts is
// one the SDK would also produce, and rejects the inputs that would otherwise
// fail server-side or double-prefix:
//   - empty or whitespace-padded values
//   - the URN-reserved characters ',', '(', ')' and U+241F
//   - values already starting with "urn:li:" (the SDK coerces these; the
//     provider rejects them so a full URN pasted into an id attribute fails
//     loudly instead of nesting)
type urnIDValidator struct {
	// attribute names the attribute in diagnostics, e.g. "server_id".
	attribute string
}

func (v urnIDValidator) Description(_ context.Context) string {
	return "must be a non-empty id without URN-reserved characters (',', '(', ')', U+241F), without leading or trailing whitespace, and not a full URN"
}

func (v urnIDValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v urnIDValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	got := req.ConfigValue.ValueString()

	if got == "" {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid id",
			fmt.Sprintf("%s must not be empty.", v.attribute),
		)
		return
	}
	if strings.TrimSpace(got) != got {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid id",
			fmt.Sprintf("%s must not have leading or trailing whitespace.", v.attribute),
		)
		return
	}
	if strings.HasPrefix(got, "urn:li:") {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid id",
			fmt.Sprintf("%s is the bare id, not a full URN; remove the \"urn:li:...\" prefix.", v.attribute),
		)
		return
	}
	for _, reserved := range []rune{',', '(', ')', '\u241F'} {
		if strings.ContainsRune(got, reserved) {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Invalid id",
				fmt.Sprintf("%s contains the URN-reserved character %q, which DataHub rejects.", v.attribute, reserved),
			)
			return
		}
	}
}
