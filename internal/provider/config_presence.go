// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// configSupplies reports whether the configuration supplies a value for a string
// attribute. It is for conditional-requirement checks in ValidateConfig - the
// "required when type = X" and "only valid when type = X" rules.
//
// An unknown value counts as supplied. The practitioner wrote something; it
// simply cannot be read until apply, because it came from a variable, a
// for_each, or another resource's attribute. Both directions of the check need
// that reading:
//
//   - "required when" - an unknown must not be reported as missing. It is set.
//   - "only valid when" - an unknown must still be reported as unexpected. It is
//     set, in a place it does not belong, and the value is irrelevant to that.
//
// Writing the test as
//
//	!v.IsNull() && v.ValueString() != ""
//
// gets both wrong, because ValueString() returns "" for an unknown value and
// nothing errors. That produced a "Missing x" diagnostic for an attribute the
// configuration set, pointing at the very line that set it.
//
// Note the numeric checks alongside these already behave correctly by using
// IsNull() alone, which treats unknown as supplied. This helper makes the string
// checks agree with them rather than introducing a new convention.
func configSupplies(v types.String) bool {
	if v.IsUnknown() {
		return true
	}
	return !v.IsNull() && v.ValueString() != ""
}
