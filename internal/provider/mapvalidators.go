// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// nonEmptyStringMapValidator rejects an empty map and any empty key, null value,
// or empty-string value in a string-map attribute (e.g. custom_properties).
// These inputs are either silently coerced (a null value becomes "") or produce
// perpetual drift (an empty map reads back as null), so failing fast at plan
// time with a clear message is preferable to accepting them. Shared by
// datahub_domain and datahub_data_product.
type nonEmptyStringMapValidator struct{}

func (v nonEmptyStringMapValidator) Description(_ context.Context) string {
	return "must be omitted or contain only non-empty string keys and non-null, non-empty string values"
}

func (v nonEmptyStringMapValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v nonEmptyStringMapValidator) ValidateMap(_ context.Context, req validator.MapRequest, resp *validator.MapResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	elems := req.ConfigValue.Elements()
	if len(elems) == 0 {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Empty map not allowed",
			"This attribute must not be set to an empty map. Omit it entirely to attach no properties.",
		)
		return
	}
	for k, val := range elems {
		if k == "" {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Empty key not allowed",
				"This attribute must not contain an empty string key.",
			)
		}
		sv, ok := val.(types.String)
		if !ok {
			continue
		}
		if sv.IsUnknown() {
			continue // cannot validate a value that is not yet known
		}
		if sv.IsNull() {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Null value not allowed",
				fmt.Sprintf("The value for key %q is null. Provide a non-empty string, or remove the key.", k),
			)
			continue
		}
		if sv.ValueString() == "" {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Empty value not allowed",
				fmt.Sprintf("The value for key %q is an empty string. Provide a non-empty string, or remove the key.", k),
			)
		}
	}
}
