// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
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

func TestConfigSuppliesSet(t *testing.T) {
	t.Parallel()

	known := func(vals ...string) types.Set {
		elems := make([]attr.Value, 0, len(vals))
		for _, v := range vals {
			elems = append(elems, types.StringValue(v))
		}
		s, d := types.SetValue(types.StringType, elems)
		if d.HasError() {
			t.Fatalf("building set: %v", d)
		}
		return s
	}

	cases := []struct {
		name string
		v    types.Set
		want bool
	}{
		{"non-empty set is supplied", known("a"), true},
		{"empty set is not supplied", known(), false},
		{"null set is not supplied", types.SetNull(types.StringType), false},

		// The broken shape: a wholly unknown collection reports zero elements.
		{"wholly unknown set is supplied", types.SetUnknown(types.StringType), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := configSuppliesSet(tc.v); got != tc.want {
				t.Errorf("configSuppliesSet(%v) = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}

// A known collection holding an unknown element already reports its true length,
// so it was never part of the defect. Pinning it stops someone "fixing" a
// non-problem by treating any set containing an unknown as unknown, which would
// change behaviour for configurations that work correctly today.
func TestConfigSuppliesSetWithUnknownElement(t *testing.T) {
	t.Parallel()

	s, d := types.SetValue(types.StringType, []attr.Value{types.StringUnknown()})
	if d.HasError() {
		t.Fatalf("building set: %v", d)
	}
	if len(s.Elements()) != 1 {
		t.Fatalf("a known set holding an unknown element should report 1 element, got %d", len(s.Elements()))
	}
	if !configSuppliesSet(s) {
		t.Error("a known set holding an unknown element is supplied")
	}
}

func TestConfigSuppliesTrueBool(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		v    types.Bool
		want bool
	}{
		{"true is supplied", types.BoolValue(true), true},

		// Not a pure presence test: false is a meaningful, harmless setting, so it
		// must not trip a conflict.
		{"false is not a conflict", types.BoolValue(false), false},
		{"null is not a conflict", types.BoolNull(), false},

		// Errs toward reporting: accepting an unknown that resolves true ships an
		// over-permissive policy, whereas a false positive costs one edit.
		{"unknown may turn out true, so report", types.BoolUnknown(), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := configSuppliesTrueBool(tc.v); got != tc.want {
				t.Errorf("configSuppliesTrueBool(%v) = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}
