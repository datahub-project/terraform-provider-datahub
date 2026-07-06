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

func TestServiceAccountIDValidator(t *testing.T) {
	cases := []struct {
		name    string
		id      types.String
		wantErr bool
	}{
		{"valid", types.StringValue("ci-bot"), false},
		{"valid_dots_underscores", types.StringValue("ci.bot_1-2"), false},
		{"empty", types.StringValue(""), true},
		{"whitespace", types.StringValue("   "), true},
		{"leading_service_prefix", types.StringValue("service_ci-bot"), true},
		{"bad_charset_space", types.StringValue("ci bot"), true},
		{"bad_charset_punct", types.StringValue("ci-bot!"), true},
		{"null_skipped", types.StringNull(), false},
		{"unknown_skipped", types.StringUnknown(), false},
	}

	v := serviceAccountIDValidator{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &validator.StringResponse{}
			v.ValidateString(context.Background(), validator.StringRequest{
				Path:        path.Root("service_account_id"),
				ConfigValue: tc.id,
			}, resp)
			if got := resp.Diagnostics.HasError(); got != tc.wantErr {
				t.Errorf("HasError = %v, want %v (diags: %v)", got, tc.wantErr, resp.Diagnostics)
			}
		})
	}

	if v.Description(context.Background()) == "" {
		t.Error("Description() is empty")
	}
	if v.MarkdownDescription(context.Background()) == "" {
		t.Error("MarkdownDescription() is empty")
	}
}
