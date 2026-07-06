// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestEnumStringValidator(t *testing.T) {
	v := enumString("ENABLED", "DISABLED")

	cases := []struct {
		name    string
		value   types.String
		wantErr bool
	}{
		{"valid", types.StringValue("ENABLED"), false},
		{"invalid", types.StringValue("PAUSED"), true},
		{"null_skipped", types.StringNull(), false},
		{"unknown_skipped", types.StringUnknown(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &validator.StringResponse{}
			v.ValidateString(context.Background(), validator.StringRequest{
				Path:        path.Root("mode"),
				ConfigValue: tc.value,
			}, resp)
			if got := resp.Diagnostics.HasError(); got != tc.wantErr {
				t.Errorf("HasError = %v, want %v (diags: %v)", got, tc.wantErr, resp.Diagnostics)
			}
		})
	}

	if v.Description(context.Background()) == "" || v.MarkdownDescription(context.Background()) == "" {
		t.Error("Description/MarkdownDescription is empty")
	}
}

func TestEnumListValidator(t *testing.T) {
	v := enumList("RAISE_INCIDENT", "RESOLVE_INCIDENT")

	list := func(vals ...string) types.List {
		elems := make([]attr.Value, len(vals))
		for i, s := range vals {
			elems[i] = types.StringValue(s)
		}
		l, _ := types.ListValue(types.StringType, elems)
		return l
	}

	cases := []struct {
		name    string
		value   types.List
		wantErr bool
	}{
		{"valid", list("RAISE_INCIDENT"), false},
		{"valid_multi", list("RAISE_INCIDENT", "RESOLVE_INCIDENT"), false},
		{"invalid_element", list("RAISE_INCIDENT", "NUKE_IT"), true},
		{"null_skipped", types.ListNull(types.StringType), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &validator.ListResponse{}
			v.ValidateList(context.Background(), validator.ListRequest{
				Path:        path.Root("on_failure_actions"),
				ConfigValue: tc.value,
			}, resp)
			if got := resp.Diagnostics.HasError(); got != tc.wantErr {
				t.Errorf("HasError = %v, want %v (diags: %v)", got, tc.wantErr, resp.Diagnostics)
			}
		})
	}

	if v.Description(context.Background()) == "" || v.MarkdownDescription(context.Background()) == "" {
		t.Error("Description/MarkdownDescription is empty")
	}
}
