// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// TestStructuredPropertyReplacementWarning pins the plan-time warning for a
// forced replacement. The destroy half of a replacement succeeds before the
// create half can fail, so the warning must say the property and its values
// are deleted, and, when version is unchanged, that the re-create is
// refused on DataHub v1.8.0 and later.
func TestStructuredPropertyReplacementWarning(t *testing.T) {
	ctx := context.Background()
	var sr resource.SchemaResponse
	(&structuredPropertyResource{}).Schema(ctx, resource.SchemaRequest{}, &sr)
	objType := sr.Schema.Type().TerraformType(ctx)

	withVersion := func(v string) (tfsdk.Plan, tfsdk.State) {
		plan := tfsdk.Plan{Schema: sr.Schema, Raw: tftypes.NewValue(objType, nil)}
		state := tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(objType, nil)}
		if v != "" {
			if d := plan.SetAttribute(ctx, path.Root("version"), v); d.HasError() {
				t.Fatalf("setting plan version: %v", d)
			}
		}
		return plan, state
	}

	for _, tc := range []struct {
		name        string
		planVersion string
		wantRefusal bool
	}{
		{"version unchanged", "", true},
		{"version raised", "20260101000000", false},
	} {
		plan, state := withVersion(tc.planVersion)
		two := types.ListValueMust(types.StringType, []attr.Value{types.StringValue("a"), types.StringValue("b")})
		one := types.ListValueMust(types.StringType, []attr.Value{types.StringValue("a")})
		twoSet := types.SetValueMust(types.StringType, []attr.Value{types.StringValue("a"), types.StringValue("b")})
		oneSet := types.SetValueMust(types.StringType, []attr.Value{types.StringValue("a")})
		// Every modifier that forces a replacement must also warn.
		modifiers := map[string]func() (bool, diag.Diagnostics){
			"allowed_values shrink": func() (bool, diag.Diagnostics) {
				var resp planmodifier.ListResponse
				requiresReplaceIfListShrunkModifier{}.PlanModifyList(ctx, planmodifier.ListRequest{Plan: plan, State: state, StateValue: two, PlanValue: one}, &resp)
				return resp.RequiresReplace, resp.Diagnostics
			},
			"set shrink": func() (bool, diag.Diagnostics) {
				var resp planmodifier.SetResponse
				requiresReplaceIfSetShrunkModifier{}.PlanModifySet(ctx, planmodifier.SetRequest{Plan: plan, State: state, StateValue: twoSet, PlanValue: oneSet}, &resp)
				return resp.RequiresReplace, resp.Diagnostics
			},
			"cardinality narrowed": func() (bool, diag.Diagnostics) {
				var resp planmodifier.StringResponse
				requiresReplaceIfNarrowedModifier{}.PlanModifyString(ctx, planmodifier.StringRequest{Plan: plan, State: state, StateValue: types.StringValue("MULTIPLE"), PlanValue: types.StringValue("SINGLE")}, &resp)
				return resp.RequiresReplace, resp.Diagnostics
			},
		}
		for name, run := range modifiers {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				replace, diags := run()
				if !replace {
					t.Fatalf("expected a forced replacement")
				}
				if diags.WarningsCount() != 1 || diags.HasError() {
					t.Fatalf("a forced replacement must carry exactly one warning, got %v", diags)
				}
				detail := diags.Warnings()[0].Detail()
				if !strings.Contains(detail, "every value applied to assets") {
					t.Errorf("the warning must say applied values are removed: %s", detail)
				}
				if got := strings.Contains(detail, "would delete the property and then fail"); got != tc.wantRefusal {
					t.Errorf("refusal sentence present=%v, want %v: %s", got, tc.wantRefusal, detail)
				}
			})
		}
	}
}
