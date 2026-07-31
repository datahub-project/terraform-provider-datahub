// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestConfigSupplies(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		v    types.String
		want bool
	}{
		{"a value is supplied", types.StringValue("DAY"), true},

		// The case the bug turned on. Before this helper the test was
		// !IsNull() && ValueString() != "", and ValueString() returns "" for an
		// unknown, so a set-but-unresolved attribute read as absent.
		{"unknown is supplied - it is set, just not readable yet", types.StringUnknown(), true},

		{"null is not supplied", types.StringNull(), false},

		// Empty string is treated as absent deliberately, matching the previous
		// behaviour for known values: these attributes are enum-like, so "" is
		// never a meaningful setting.
		{"empty string is not supplied", types.StringValue(""), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := configSupplies(tc.v); got != tc.want {
				t.Errorf("configSupplies(%v) = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}

// TestConfigSuppliesMatchesNumericConvention pins the reason this helper reads
// unknown as supplied: the numeric checks in the same ValidateConfig functions
// already do, by testing IsNull() alone. The string checks disagreed, which is
// the whole defect. If someone "simplifies" configSupplies to drop the unknown
// branch, the two conventions diverge again and the bug returns.
func TestConfigSuppliesMatchesNumericConvention(t *testing.T) {
	t.Parallel()

	// How the numeric attributes are tested today, verbatim.
	numericSupplies := func(v types.Int64) bool { return !v.IsNull() }

	if numericSupplies(types.Int64Unknown()) != configSupplies(types.StringUnknown()) {
		t.Error("string and numeric presence checks disagree on an unknown value; " +
			"that divergence is what produced a \"Missing x\" error for an attribute the config set")
	}
	if numericSupplies(types.Int64Null()) != configSupplies(types.StringNull()) {
		t.Error("string and numeric presence checks disagree on a null value")
	}
}
