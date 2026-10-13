// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestCheckDatasetURN(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"simple", "urn:li:dataset:(urn:li:dataPlatform:snowflake,db.schema.table,PROD)", false},
		{"dotted_name", "urn:li:dataset:(urn:li:dataPlatform:postgres,a.b.c.d,PROD)", false},
		{"name_with_parens_and_comma", "urn:li:dataset:(urn:li:dataPlatform:hive,tbl(a,b),PROD)", false},
		{"env_dev", "urn:li:dataset:(urn:li:dataPlatform:hive,t,DEV)", false},
		{"env_non_prod", "urn:li:dataset:(urn:li:dataPlatform:hive,t,NON_PROD)", false},
		{"env_unlisted_is_accepted", "urn:li:dataset:(urn:li:dataPlatform:hive,t,SANDBOX)", false},

		{"empty", "", true},
		{"missing_prefix", "urn:li:dataFlow:(urn:li:dataPlatform:hive,t,PROD)", true},
		{"bare_name", "db.schema.table", true},
		{"missing_close_paren", "urn:li:dataset:(urn:li:dataPlatform:hive,t,PROD", true},
		{"two_parts", "urn:li:dataset:(urn:li:dataPlatform:hive,t)", true},
		{"four_parts", "urn:li:dataset:(urn:li:dataPlatform:hive,t,PROD,extra)", true},
		{"platform_not_urn", "urn:li:dataset:(hive,t,PROD)", true},
		{"empty_platform_id", "urn:li:dataset:(urn:li:dataPlatform:,t,PROD)", true},
		{"empty_name", "urn:li:dataset:(urn:li:dataPlatform:hive,,PROD)", true},
		{"lowercase_env", "urn:li:dataset:(urn:li:dataPlatform:hive,t,prod)", true},
		{"empty_env", "urn:li:dataset:(urn:li:dataPlatform:hive,t,)", true},
		{"unbalanced_name_parens", "urn:li:dataset:(urn:li:dataPlatform:hive,tbl(a,PROD)", true},
		{"stray_close_paren_in_name", "urn:li:dataset:(urn:li:dataPlatform:hive,a)b,PROD)", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkDatasetURN(tc.in)
			if (got != "") != tc.wantErr {
				t.Errorf("checkDatasetURN(%q) = %q, wantErr %v", tc.in, got, tc.wantErr)
			}
		})
	}
}

func TestDatasetURNValidator(t *testing.T) {
	v := datasetURN()
	cases := []struct {
		name    string
		value   types.String
		wantErr bool
	}{
		{"valid", types.StringValue("urn:li:dataset:(urn:li:dataPlatform:hive,t,PROD)"), false},
		{"invalid", types.StringValue("nope"), true},
		{"null_skipped", types.StringNull(), false},
		{"unknown_skipped", types.StringUnknown(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &validator.StringResponse{}
			v.ValidateString(context.Background(), validator.StringRequest{
				Path:        path.Root("entity_urn"),
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
