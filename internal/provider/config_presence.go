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

// configSuppliesSet reports whether the configuration supplies a non-empty
// collection, on the same reading as configSupplies: an unknown counts as
// supplied.
//
// The distinction that matters here is between a collection that is itself
// unknown and one that is known but holds unknown elements. Only the former
// reads as empty from Elements(); the latter already reports its true length. So
// a wholly computed set is what this guards, and a literal list containing a
// computed element needed no guarding.
func configSuppliesSet(v types.Set) bool {
	if v.IsUnknown() {
		return true
	}
	return !v.IsNull() && len(v.Elements()) > 0
}

// configSuppliesTrueBool reports whether the configuration supplies a bool that
// is, or may turn out to be, true.
//
// Unlike the string and set cases this is not a pure presence test: the callers
// care specifically about a true value, since all_resources = false is a
// meaningful and harmless setting. An unknown counts as possibly-true, because
// accepting it and being wrong ships an over-permissive policy, whereas
// rejecting it and being wrong costs the practitioner one edit. Asymmetric
// consequences, so the check errs toward reporting.
func configSuppliesTrueBool(v types.Bool) bool {
	if v.IsUnknown() {
		return true
	}
	return !v.IsNull() && v.ValueBool()
}
