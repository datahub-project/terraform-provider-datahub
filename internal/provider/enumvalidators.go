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

// enumStringValidator rejects a string attribute whose value is not one of a
// fixed allowed set. Reproduces the OneOf validator from the
// terraform-plugin-framework-validators module without taking that dependency,
// matching the project convention of hand-rolled validators on the core package.
type enumStringValidator struct {
	allowed []string
}

func enumString(allowed ...string) enumStringValidator {
	return enumStringValidator{allowed: allowed}
}

func (v enumStringValidator) Description(_ context.Context) string {
	return fmt.Sprintf("value must be one of: %s", strings.Join(v.allowed, ", "))
}

func (v enumStringValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v enumStringValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	got := req.ConfigValue.ValueString()
	for _, a := range v.allowed {
		if got == a {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Invalid value",
		fmt.Sprintf("%q is not valid; expected one of: %s", got, strings.Join(v.allowed, ", ")),
	)
}

// enumListValidator applies enum membership to every element of a string list.
type enumListValidator struct {
	allowed []string
}

func enumList(allowed ...string) enumListValidator {
	return enumListValidator{allowed: allowed}
}

func (v enumListValidator) Description(_ context.Context) string {
	return fmt.Sprintf("each element must be one of: %s", strings.Join(v.allowed, ", "))
}

func (v enumListValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v enumListValidator) ValidateList(_ context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	for _, elem := range req.ConfigValue.Elements() {
		sv, ok := elem.(types.String)
		if !ok || sv.IsNull() || sv.IsUnknown() {
			continue
		}
		got := sv.ValueString()
		valid := false
		for _, a := range v.allowed {
			if got == a {
				valid = true
				break
			}
		}
		if !valid {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Invalid value",
				fmt.Sprintf("%q is not valid; expected one of: %s", got, strings.Join(v.allowed, ", ")),
			)
		}
	}
}

// enumSetValidator applies enum membership to every element of a string set.
// An empty set is valid (used e.g. by auto_properties, where [] means
// "disable").
type enumSetValidator struct {
	allowed []string
}

func enumSet(allowed ...string) enumSetValidator {
	return enumSetValidator{allowed: allowed}
}

func (v enumSetValidator) Description(_ context.Context) string {
	return fmt.Sprintf("each element must be one of: %s", strings.Join(v.allowed, ", "))
}

func (v enumSetValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v enumSetValidator) ValidateSet(_ context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	for _, elem := range req.ConfigValue.Elements() {
		sv, ok := elem.(types.String)
		if !ok || sv.IsNull() || sv.IsUnknown() {
			continue
		}
		got := sv.ValueString()
		valid := false
		for _, a := range v.allowed {
			if got == a {
				valid = true
				break
			}
		}
		if !valid {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Invalid value",
				fmt.Sprintf("%q is not valid; expected one of: %s", got, strings.Join(v.allowed, ", ")),
			)
		}
	}
}
