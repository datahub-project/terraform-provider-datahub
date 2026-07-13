// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// This file implements the provider-level defaults engine: the parsed form of
// the provider's `defaults`, `auto_properties`, and `auto_property_strategy`
// configuration, the per-entity-type support matrix, and the pure plan-time
// merge functions the affected resources call from ModifyPlan.
//
// Design doc: docs/design/provider-default-labels.md. The one invariant that
// must never be violated: merge results are a pure function of resource
// config, prior Terraform state, and provider configuration - never of
// server-side data. Anything else risks "Provider produced inconsistent final
// plan" errors when values resolve between plan and apply.

const (
	// Auto-property marker names accepted in `auto_properties`.
	autoPropertyManagedBy       = "managed-by"
	autoPropertyProviderVersion = "provider-version"

	// managedByValue is the value written for the managed-by marker.
	managedByValue = "terraform"

	// Values accepted in `auto_property_strategy`.
	autoPropertyStrategyCreationOnly = "CREATION_ONLY"
	autoPropertyStrategyProactive    = "PROACTIVE"
)

// tagURNPrefix and structuredPropertyURNPrefix are declared in
// tag_resource.go and structured_property_resource.go respectively and are
// reused by the validators below.

// entityKind identifies a DataHub entity type managed by this provider for
// the purpose of the defaults support matrix.
type entityKind int

const (
	kindDomain entityKind = iota
	kindGlossaryTerm
	kindGlossaryNode
	kindCorpUser // also covers datahub_service_account
	kindCorpGroup
	kindDataProduct
	kindDataContract
	kindAssertion
)

// allEntityKinds enumerates every entityKind; the matrix completeness test
// asserts defaultsSupport has a row for each.
var allEntityKinds = []entityKind{
	kindDomain,
	kindGlossaryTerm,
	kindGlossaryNode,
	kindCorpUser,
	kindCorpGroup,
	kindDataProduct,
	kindDataContract,
	kindAssertion,
}

// kindSupport records which default-label mechanisms a DataHub entity type
// accepts. Derived from the entity registry
// (metadata-models/src/main/resources/entity-registry.yml): an aspect write to
// an entity type that does not register the aspect is rejected (or silently
// dropped) by the server.
type kindSupport struct {
	CustomProperties     bool
	StructuredProperties bool
	Tags                 bool
}

// defaultsSupport is the authoritative support matrix. Assertions register
// customProperties server-side but the value is not exposed in the DataHub UI
// or GraphQL API, so CP defaults are deliberately withheld there.
var defaultsSupport = map[entityKind]kindSupport{
	kindDomain:       {CustomProperties: true, StructuredProperties: true, Tags: false},
	kindGlossaryTerm: {CustomProperties: true, StructuredProperties: true, Tags: false},
	kindGlossaryNode: {CustomProperties: true, StructuredProperties: true, Tags: false},
	kindCorpUser:     {CustomProperties: true, StructuredProperties: true, Tags: true},
	kindCorpGroup:    {CustomProperties: false, StructuredProperties: true, Tags: true},
	kindDataProduct:  {CustomProperties: true, StructuredProperties: true, Tags: true},
	kindDataContract: {CustomProperties: false, StructuredProperties: true, Tags: false},
	kindAssertion:    {CustomProperties: false, StructuredProperties: false, Tags: true},
}

// entityDefaults is the parsed provider-level defaults configuration. Members
// are kept as framework values so unknown-ness (e.g. a default sourced from a
// not-yet-applied resource attribute) survives into plan computation, where it
// resolves to "known after apply".
type entityDefaults struct {
	// CustomProperties, Tags, and StructuredProperties come from the
	// `defaults` nested attribute; all null when the block is omitted.
	CustomProperties     types.Map // map[string]string
	Tags                 types.Set // set[string], tag URNs
	StructuredProperties types.Map // map[string]set[string], SP URN -> values

	// AutoProperties and AutoPropertyStrategy come from the top-level
	// attributes of the same names; null means "use the built-in default"
	// (["managed-by"] and CREATION_ONLY respectively).
	AutoProperties       types.Set
	AutoPropertyStrategy types.String

	// providerVersion is the running provider version, used as the value of
	// the provider-version marker.
	providerVersion string
}

// providerDefaultsModel mirrors the `defaults` nested attribute.
type providerDefaultsModel struct {
	CustomProperties     types.Map `tfsdk:"custom_properties"`
	Tags                 types.Set `tfsdk:"tags"`
	StructuredProperties types.Map `tfsdk:"structured_properties"`
}

var spDefaultsElementType = types.SetType{ElemType: types.StringType}

// emptyEntityDefaults returns an entityDefaults with every input null: the
// defaults block absent and the auto-property attributes at their built-in
// defaults. Used until the provider schema exposes the configuration.
func emptyEntityDefaults(version string) entityDefaults {
	return entityDefaults{
		CustomProperties:     types.MapNull(types.StringType),
		Tags:                 types.SetNull(types.StringType),
		StructuredProperties: types.MapNull(spDefaultsElementType),
		AutoProperties:       types.SetNull(types.StringType),
		AutoPropertyStrategy: types.StringNull(),
		providerVersion:      version,
	}
}

// parseEntityDefaults converts the provider configuration values (the
// `defaults` object plus the top-level `auto_properties` and
// `auto_property_strategy` attributes) into an entityDefaults. Unknown values
// are preserved, not rejected: unlike gms_url/gms_token they are not needed to
// build the client, and they resolve naturally during the apply-time replan.
func parseEntityDefaults(ctx context.Context, defaultsObj types.Object, autoProperties types.Set, autoPropertyStrategy types.String, version string) (entityDefaults, diag.Diagnostics) {
	var diags diag.Diagnostics
	d := emptyEntityDefaults(version)
	d.AutoProperties = autoProperties
	d.AutoPropertyStrategy = autoPropertyStrategy

	switch {
	case defaultsObj.IsNull():
		// Feature off for the defaults block; markers may still apply.
	case defaultsObj.IsUnknown():
		d.CustomProperties = types.MapUnknown(types.StringType)
		d.Tags = types.SetUnknown(types.StringType)
		d.StructuredProperties = types.MapUnknown(spDefaultsElementType)
	default:
		var m providerDefaultsModel
		diags.Append(defaultsObj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return d, diags
		}
		if !m.CustomProperties.IsNull() {
			d.CustomProperties = m.CustomProperties
		}
		if !m.Tags.IsNull() {
			d.Tags = m.Tags
		}
		if !m.StructuredProperties.IsNull() {
			d.StructuredProperties = m.StructuredProperties
		}
	}

	return d, diags
}

// autoPropertyNames returns the enabled marker names in sorted order. A null
// (omitted) auto_properties means the built-in default ["managed-by"]; an
// explicit empty set disables markers entirely.
func (d entityDefaults) autoPropertyNames() []string {
	if d.AutoProperties.IsNull() {
		return []string{autoPropertyManagedBy}
	}
	if d.AutoProperties.IsUnknown() {
		return nil
	}
	names := make([]string, 0, len(d.AutoProperties.Elements()))
	for _, e := range d.AutoProperties.Elements() {
		s, ok := e.(types.String)
		if !ok || s.IsNull() || s.IsUnknown() {
			continue
		}
		names = append(names, s.ValueString())
	}
	sort.Strings(names)
	return names
}

// autoPropertyValue returns the live value for a marker name.
func (d entityDefaults) autoPropertyValue(name string) string {
	switch name {
	case autoPropertyManagedBy:
		return managedByValue
	case autoPropertyProviderVersion:
		return d.providerVersion
	default:
		return ""
	}
}

// autoPropertyStrategy returns the effective strategy (null means
// CREATION_ONLY).
func (d entityDefaults) autoPropertyStrategy() string {
	if d.AutoPropertyStrategy.IsNull() || d.AutoPropertyStrategy.IsUnknown() {
		return autoPropertyStrategyCreationOnly
	}
	return d.AutoPropertyStrategy.ValueString()
}

// cpCollision records a resource-level custom_properties key that overrides a
// provider-level value with a different value. Reported as a plan-time
// warning; the resource value wins.
type cpCollision struct {
	Key            string
	ResourceValue  string
	ProviderValue  string
	ProviderSource string // "defaults.custom_properties" or "auto_properties"
}

// cpMergeInput carries the pure plan-time inputs to the custom-properties
// merge.
type cpMergeInput struct {
	// Config is the resource's configured custom_properties.
	Config types.Map
	// PriorAll is the prior state's custom_properties_all; null on create and
	// on states written before this feature existed.
	PriorAll types.Map
	// IsCreate is true when the plan creates the entity (prior state is null).
	IsCreate bool
}

// mergeCustomProperties computes the planned custom_properties_all value:
// auto-property markers (lowest precedence) < defaults.custom_properties <
// resource config. An empty result canonicalises to null. Collisions are
// reported only when the resource value and the provider-level value are both
// known and differ; same-value overlap is harmless layering.
func (d entityDefaults) mergeCustomProperties(in cpMergeInput) (types.Map, []cpCollision) {
	if d.CustomProperties.IsUnknown() || in.Config.IsUnknown() ||
		d.AutoProperties.IsUnknown() || d.AutoPropertyStrategy.IsUnknown() {
		return types.MapUnknown(types.StringType), nil
	}

	merged := map[string]attr.Value{}
	source := map[string]string{}

	// 1. Auto-property markers.
	for k, v := range d.plannedAutoProperties(in) {
		merged[k] = types.StringValue(v)
		source[k] = "auto_properties"
	}

	// 2. Provider default custom properties. Overriding a marker here is the
	// documented way to change a marker's value, so it is silent.
	if !d.CustomProperties.IsNull() {
		for k, v := range d.CustomProperties.Elements() {
			merged[k] = v
			source[k] = "defaults.custom_properties"
		}
	}

	// 3. Resource-level custom properties win per key.
	var collisions []cpCollision
	if !in.Config.IsNull() {
		for k, v := range in.Config.Elements() {
			if pv, exists := merged[k]; exists {
				rs, rok := v.(types.String)
				ps, pok := pv.(types.String)
				if rok && pok && !rs.IsUnknown() && !ps.IsUnknown() && !rs.Equal(ps) {
					collisions = append(collisions, cpCollision{
						Key:            k,
						ResourceValue:  rs.ValueString(),
						ProviderValue:  ps.ValueString(),
						ProviderSource: source[k],
					})
				}
			}
			merged[k] = v
		}
	}

	sort.Slice(collisions, func(i, j int) bool { return collisions[i].Key < collisions[j].Key })

	if len(merged) == 0 {
		return types.MapNull(types.StringType), collisions
	}
	out, diags := types.MapValue(types.StringType, merged)
	if diags.HasError() {
		// All elements are types.String by construction; this cannot happen.
		return types.MapUnknown(types.StringType), collisions
	}
	return out, collisions
}

// plannedAutoProperties resolves the marker key/value pairs for one plan.
//
// PROACTIVE: live values on every plan. CREATION_ONLY: live values when the
// entity is being created; otherwise each enabled marker carries forward the
// value it already has in prior state (frozen at creation), and markers absent
// from prior state stay absent. Markers removed from auto_properties are
// excluded regardless of strategy, which surfaces as a removal diff.
func (d entityDefaults) plannedAutoProperties(in cpMergeInput) map[string]string {
	names := d.autoPropertyNames()
	if len(names) == 0 {
		return nil
	}

	out := map[string]string{}
	if d.autoPropertyStrategy() == autoPropertyStrategyProactive || in.IsCreate {
		for _, name := range names {
			out[name] = d.autoPropertyValue(name)
		}
		return out
	}

	if in.PriorAll.IsNull() || in.PriorAll.IsUnknown() {
		return nil
	}
	prior := in.PriorAll.Elements()
	for _, name := range names {
		if pv, ok := prior[name]; ok {
			if s, sok := pv.(types.String); sok && !s.IsNull() && !s.IsUnknown() {
				out[name] = s.ValueString()
			}
		}
	}
	return out
}

// urnPrefixSetValidator requires every element of a string set to be a URN
// with the given prefix.
type urnPrefixSetValidator struct {
	prefix string
}

func (v urnPrefixSetValidator) Description(_ context.Context) string {
	return fmt.Sprintf("each element must be a URN starting with %q", v.prefix)
}

func (v urnPrefixSetValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v urnPrefixSetValidator) ValidateSet(_ context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	for _, e := range req.ConfigValue.Elements() {
		s, ok := e.(types.String)
		if !ok || s.IsNull() || s.IsUnknown() {
			continue
		}
		if !strings.HasPrefix(s.ValueString(), v.prefix) || s.ValueString() == v.prefix {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Invalid URN",
				fmt.Sprintf("%q is not a URN of the expected type; expected a URN starting with %q.", s.ValueString(), v.prefix),
			)
		}
	}
}

// spDefaultsMapValidator validates the defaults.structured_properties map:
// keys must be structured property URNs and each value must be a non-empty
// set of non-empty strings.
type spDefaultsMapValidator struct{}

func (v spDefaultsMapValidator) Description(_ context.Context) string {
	return "keys must be structured property URNs; values must be non-empty sets of non-empty strings"
}

func (v spDefaultsMapValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v spDefaultsMapValidator) ValidateMap(_ context.Context, req validator.MapRequest, resp *validator.MapResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	elems := req.ConfigValue.Elements()
	if len(elems) == 0 {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Empty map not allowed",
			"This attribute must not be set to an empty map. Omit it entirely to attach no structured properties.",
		)
		return
	}
	for k, val := range elems {
		if !strings.HasPrefix(k, structuredPropertyURNPrefix) || k == structuredPropertyURNPrefix {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Invalid structured property URN",
				fmt.Sprintf("Key %q must be a structured property URN starting with %q.", k, structuredPropertyURNPrefix),
			)
		}
		set, ok := val.(types.Set)
		if !ok || set.IsNull() || set.IsUnknown() {
			continue
		}
		if len(set.Elements()) == 0 {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Empty value set not allowed",
				fmt.Sprintf("The value set for %q is empty. Provide at least one value, or remove the key.", k),
			)
			continue
		}
		for _, e := range set.Elements() {
			s, sok := e.(types.String)
			if !sok || s.IsUnknown() {
				continue
			}
			if s.IsNull() || s.ValueString() == "" {
				resp.Diagnostics.AddAttributeError(
					req.Path,
					"Empty value not allowed",
					fmt.Sprintf("The value set for %q contains a null or empty string.", k),
				)
			}
		}
	}
}
