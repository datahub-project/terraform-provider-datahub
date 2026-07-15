// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func stringMap(m map[string]string) types.Map {
	elems := map[string]attr.Value{}
	for k, v := range m {
		elems[k] = types.StringValue(v)
	}
	out, diags := types.MapValue(types.StringType, elems)
	if diags.HasError() {
		panic(diags)
	}
	return out
}

func stringSet(vals ...string) types.Set {
	elems := make([]attr.Value, 0, len(vals))
	for _, v := range vals {
		elems = append(elems, types.StringValue(v))
	}
	out, diags := types.SetValue(types.StringType, elems)
	if diags.HasError() {
		panic(diags)
	}
	return out
}

// testDefaults returns an entityDefaults with everything null (feature off
// apart from the built-in auto-property default) and version 1.2.3.
func testDefaults() entityDefaults {
	return entityDefaults{
		CustomProperties:     types.MapNull(types.StringType),
		Tags:                 types.SetNull(types.StringType),
		StructuredProperties: types.MapNull(spDefaultsElementType),
		AutoProperties:       types.SetNull(types.StringType),
		AutoPropertyStrategy: types.StringNull(),
		providerVersion:      "1.2.3",
	}
}

func requireMapEquals(t *testing.T, got types.Map, want map[string]string) {
	t.Helper()
	if got.IsNull() || got.IsUnknown() {
		t.Fatalf("expected known map %v, got %s", want, got)
	}
	elems := got.Elements()
	if len(elems) != len(want) {
		t.Fatalf("expected %d entries %v, got %d: %s", len(want), want, len(elems), got)
	}
	for k, v := range want {
		e, ok := elems[k]
		if !ok {
			t.Fatalf("missing key %q in %s", k, got)
		}
		s, ok := e.(types.String)
		if !ok || s.ValueString() != v {
			t.Fatalf("key %q: expected %q, got %s", k, v, e)
		}
	}
}

func TestDefaultsSupportMatrixComplete(t *testing.T) {
	for _, kind := range allEntityKinds {
		if _, ok := defaultsSupport[kind]; !ok {
			t.Errorf("defaultsSupport is missing a row for entityKind %d", kind)
		}
	}
	if len(defaultsSupport) != len(allEntityKinds) {
		t.Errorf("defaultsSupport has %d rows, allEntityKinds has %d", len(defaultsSupport), len(allEntityKinds))
	}
}

func TestMergeCustomPropertiesMarkerOnCreate(t *testing.T) {
	d := testDefaults()
	merged, collisions := d.mergeCustomProperties(cpMergeInput{
		Config:   types.MapNull(types.StringType),
		PriorAll: types.MapNull(types.StringType),
		IsCreate: true,
	})
	requireMapEquals(t, merged, map[string]string{"managed-by": "terraform"})
	if len(collisions) != 0 {
		t.Fatalf("expected no collisions, got %v", collisions)
	}
}

func TestMergeCustomPropertiesProviderVersionMarker(t *testing.T) {
	d := testDefaults()
	d.AutoProperties = stringSet(autoPropertyManagedBy, autoPropertyProviderVersion)
	merged, _ := d.mergeCustomProperties(cpMergeInput{
		Config:   types.MapNull(types.StringType),
		PriorAll: types.MapNull(types.StringType),
		IsCreate: true,
	})
	requireMapEquals(t, merged, map[string]string{
		"managed-by":       "terraform",
		"provider-version": "1.2.3",
	})
}

func TestMergeCustomPropertiesCreationOnlySilentOnExisting(t *testing.T) {
	// Existing resource (created before the feature, no markers in prior
	// state) must plan a null merge under CREATION_ONLY: zero upgrade diffs.
	d := testDefaults()
	merged, _ := d.mergeCustomProperties(cpMergeInput{
		Config:   types.MapNull(types.StringType),
		PriorAll: types.MapNull(types.StringType),
		IsCreate: false,
	})
	if !merged.IsNull() {
		t.Fatalf("expected null merge for unstamped existing resource, got %s", merged)
	}
}

func TestMergeCustomPropertiesCreationOnlyFreezesValues(t *testing.T) {
	// A stamped resource carries its creation-time marker values forward even
	// when the live values have moved on (provider upgraded 1.0.0 -> 1.2.3).
	d := testDefaults()
	d.AutoProperties = stringSet(autoPropertyManagedBy, autoPropertyProviderVersion)
	merged, _ := d.mergeCustomProperties(cpMergeInput{
		Config: types.MapNull(types.StringType),
		PriorAll: stringMap(map[string]string{
			"managed-by":       "terraform",
			"provider-version": "1.0.0",
		}),
		IsCreate: false,
	})
	requireMapEquals(t, merged, map[string]string{
		"managed-by":       "terraform",
		"provider-version": "1.0.0",
	})
}

func TestMergeCustomPropertiesProactiveRefreshesValues(t *testing.T) {
	d := testDefaults()
	d.AutoProperties = stringSet(autoPropertyManagedBy, autoPropertyProviderVersion)
	d.AutoPropertyStrategy = types.StringValue(autoPropertyStrategyProactive)
	merged, _ := d.mergeCustomProperties(cpMergeInput{
		Config: types.MapNull(types.StringType),
		PriorAll: stringMap(map[string]string{
			"managed-by":       "terraform",
			"provider-version": "1.0.0",
		}),
		IsCreate: false,
	})
	requireMapEquals(t, merged, map[string]string{
		"managed-by":       "terraform",
		"provider-version": "1.2.3",
	})
}

func TestMergeCustomPropertiesMarkerRemovalIgnoresStrategy(t *testing.T) {
	// auto_properties = [] drops markers even under CREATION_ONLY with a
	// stamped prior state: explicit removal is honored estate-wide.
	d := testDefaults()
	d.AutoProperties = stringSet() // explicit empty: disable
	merged, _ := d.mergeCustomProperties(cpMergeInput{
		Config:   types.MapNull(types.StringType),
		PriorAll: stringMap(map[string]string{"managed-by": "terraform"}),
		IsCreate: false,
	})
	if !merged.IsNull() {
		t.Fatalf("expected null merge after marker disable, got %s", merged)
	}
}

func TestMergeCustomPropertiesDefaultKeyRemoval(t *testing.T) {
	// A key that was previously applied via defaults.custom_properties (and
	// so is present in prior state _all) must disappear from the merge once
	// removed from the provider defaults: defaults are never carried forward
	// from prior state - only markers are. Resource-owned keys and stamped
	// markers survive.
	d := testDefaults() // defaults.custom_properties now empty
	merged, _ := d.mergeCustomProperties(cpMergeInput{
		Config: stringMap(map[string]string{"tier": "gold"}),
		PriorAll: stringMap(map[string]string{
			"managed-by": "terraform",
			"team":       "platform", // was default-sourced; default since removed
			"tier":       "gold",
		}),
		IsCreate: false,
	})
	requireMapEquals(t, merged, map[string]string{
		"managed-by": "terraform",
		"tier":       "gold",
	})
}

func TestMergeCustomPropertiesPartialMarkerRemoval(t *testing.T) {
	// Dropping one marker from auto_properties while keeping another removes
	// only the dropped marker, even under CREATION_ONLY with both stamped in
	// prior state.
	d := testDefaults()
	d.AutoProperties = stringSet(autoPropertyManagedBy) // provider-version removed
	merged, _ := d.mergeCustomProperties(cpMergeInput{
		Config: types.MapNull(types.StringType),
		PriorAll: stringMap(map[string]string{
			"managed-by":       "terraform",
			"provider-version": "1.0.0",
		}),
		IsCreate: false,
	})
	requireMapEquals(t, merged, map[string]string{
		"managed-by": "terraform",
	})
}

func TestMergeCustomPropertiesDisabledMarkersOnCreate(t *testing.T) {
	// The plain opt-out journey: auto_properties = [] on a fresh resource.
	// Resource-level custom properties pass through untouched, and with no
	// resource properties the merge is null (nothing written at all).
	d := testDefaults()
	d.AutoProperties = stringSet()
	merged, collisions := d.mergeCustomProperties(cpMergeInput{
		Config:   stringMap(map[string]string{"tier": "gold"}),
		PriorAll: types.MapNull(types.StringType),
		IsCreate: true,
	})
	requireMapEquals(t, merged, map[string]string{"tier": "gold"})
	if len(collisions) != 0 {
		t.Fatalf("expected no collisions, got %v", collisions)
	}

	empty, _ := d.mergeCustomProperties(cpMergeInput{
		Config:   types.MapNull(types.StringType),
		PriorAll: types.MapNull(types.StringType),
		IsCreate: true,
	})
	if !empty.IsNull() {
		t.Fatalf("expected null merge with markers disabled and no config, got %s", empty)
	}
}

func TestMergeCustomPropertiesDefaultsOverrideMarkersSilently(t *testing.T) {
	d := testDefaults()
	d.CustomProperties = stringMap(map[string]string{"managed-by": "terraform-stack-a"})
	merged, collisions := d.mergeCustomProperties(cpMergeInput{
		Config:   types.MapNull(types.StringType),
		PriorAll: types.MapNull(types.StringType),
		IsCreate: true,
	})
	requireMapEquals(t, merged, map[string]string{"managed-by": "terraform-stack-a"})
	if len(collisions) != 0 {
		t.Fatalf("defaults overriding a marker must be silent, got %v", collisions)
	}
}

func TestMergeCustomPropertiesResourceWinsWithCollisionWarning(t *testing.T) {
	d := testDefaults()
	d.CustomProperties = stringMap(map[string]string{"team": "platform", "env": "prod"})
	merged, collisions := d.mergeCustomProperties(cpMergeInput{
		Config:   stringMap(map[string]string{"team": "analytics"}),
		PriorAll: types.MapNull(types.StringType),
		IsCreate: true,
	})
	requireMapEquals(t, merged, map[string]string{
		"managed-by": "terraform",
		"team":       "analytics",
		"env":        "prod",
	})
	if len(collisions) != 1 {
		t.Fatalf("expected 1 collision, got %v", collisions)
	}
	c := collisions[0]
	if c.Key != "team" || c.ResourceValue != "analytics" || c.ProviderValue != "platform" || c.ProviderSource != "defaults.custom_properties" {
		t.Fatalf("unexpected collision detail: %+v", c)
	}
}

func TestMergeCustomPropertiesSameValueOverlapIsSilent(t *testing.T) {
	// The AWS perpetual-diff trap: same key, same value at both levels must
	// produce neither a diff surprise nor a warning.
	d := testDefaults()
	d.CustomProperties = stringMap(map[string]string{"env": "prod"})
	merged, collisions := d.mergeCustomProperties(cpMergeInput{
		Config:   stringMap(map[string]string{"env": "prod"}),
		PriorAll: types.MapNull(types.StringType),
		IsCreate: true,
	})
	requireMapEquals(t, merged, map[string]string{
		"managed-by": "terraform",
		"env":        "prod",
	})
	if len(collisions) != 0 {
		t.Fatalf("same-value overlap must not warn, got %v", collisions)
	}
}

func TestMergeCustomPropertiesResourceOverridesMarkerWithWarning(t *testing.T) {
	d := testDefaults()
	merged, collisions := d.mergeCustomProperties(cpMergeInput{
		Config:   stringMap(map[string]string{"managed-by": "helm"}),
		PriorAll: types.MapNull(types.StringType),
		IsCreate: true,
	})
	requireMapEquals(t, merged, map[string]string{"managed-by": "helm"})
	if len(collisions) != 1 || collisions[0].ProviderSource != "auto_properties" {
		t.Fatalf("expected one auto_properties collision, got %v", collisions)
	}
}

func TestMergeCustomPropertiesUnknownConfigYieldsUnknown(t *testing.T) {
	d := testDefaults()
	merged, _ := d.mergeCustomProperties(cpMergeInput{
		Config:   types.MapUnknown(types.StringType),
		PriorAll: types.MapNull(types.StringType),
		IsCreate: true,
	})
	if !merged.IsUnknown() {
		t.Fatalf("expected unknown merge for unknown config, got %s", merged)
	}
}

func TestMergeCustomPropertiesUnknownDefaultsYieldUnknown(t *testing.T) {
	d := testDefaults()
	d.CustomProperties = types.MapUnknown(types.StringType)
	merged, _ := d.mergeCustomProperties(cpMergeInput{
		Config:   types.MapNull(types.StringType),
		PriorAll: types.MapNull(types.StringType),
		IsCreate: true,
	})
	if !merged.IsUnknown() {
		t.Fatalf("expected unknown merge for unknown defaults, got %s", merged)
	}
}

func TestMergeCustomPropertiesUnknownElementPreserved(t *testing.T) {
	// A per-element unknown (e.g. custom_properties = { build = random_id... })
	// stays unknown inside a known map; keys remain visible in the diff.
	d := testDefaults()
	cfg, diags := types.MapValue(types.StringType, map[string]attr.Value{
		"build": types.StringUnknown(),
	})
	if diags.HasError() {
		t.Fatal(diags)
	}
	merged, collisions := d.mergeCustomProperties(cpMergeInput{
		Config:   cfg,
		PriorAll: types.MapNull(types.StringType),
		IsCreate: true,
	})
	if merged.IsNull() || merged.IsUnknown() {
		t.Fatalf("expected known map with unknown element, got %s", merged)
	}
	if len(merged.Elements()) != 2 {
		t.Fatalf("expected 2 entries (managed-by + build), got %s", merged)
	}
	if s, ok := merged.Elements()["managed-by"].(types.String); !ok || s.ValueString() != "terraform" {
		t.Fatalf("expected managed-by marker, got %s", merged)
	}
	if s, ok := merged.Elements()["build"].(types.String); !ok || !s.IsUnknown() {
		t.Fatalf("expected build to stay unknown, got %s", merged)
	}
	if len(collisions) != 0 {
		t.Fatalf("unknown values must not warn, got %v", collisions)
	}
}

func TestAutoPropertyNamesDefault(t *testing.T) {
	d := testDefaults()
	names := d.autoPropertyNames()
	if len(names) != 1 || names[0] != autoPropertyManagedBy {
		t.Fatalf("expected default [managed-by], got %v", names)
	}
}

func TestParseEntityDefaultsNullBlock(t *testing.T) {
	d, diags := parseEntityDefaults(t.Context(),
		types.ObjectNull(providerDefaultsObjectType()),
		types.SetNull(types.StringType),
		types.StringNull(),
		"9.9.9")
	if diags.HasError() {
		t.Fatal(diags)
	}
	if !d.CustomProperties.IsNull() || !d.Tags.IsNull() || !d.StructuredProperties.IsNull() {
		t.Fatalf("expected all-null defaults, got %+v", d)
	}
	if d.providerVersion != "9.9.9" {
		t.Fatalf("expected version to be recorded, got %q", d.providerVersion)
	}
}

func TestParseEntityDefaultsPopulatedBlock(t *testing.T) {
	obj, diags := types.ObjectValue(providerDefaultsObjectType(), map[string]attr.Value{
		"custom_properties": stringMap(map[string]string{"team": "platform"}),
	})
	if diags.HasError() {
		t.Fatal(diags)
	}
	d, pdiags := parseEntityDefaults(t.Context(), obj,
		types.SetNull(types.StringType), types.StringNull(), "1.0.0")
	if pdiags.HasError() {
		t.Fatal(pdiags)
	}
	requireMapEquals(t, d.CustomProperties, map[string]string{"team": "platform"})
	if !d.Tags.IsNull() || !d.StructuredProperties.IsNull() {
		t.Fatalf("expected null tags and structured properties until their phases land, got %+v", d)
	}
}

func TestEmptyEntityDefaultsIsInert(t *testing.T) {
	// The wrapper handed to resources before the provider schema lands (and
	// whenever the feature is unconfigured) must merge to exactly the marker
	// default on create and to null on existing resources.
	d := emptyEntityDefaults("0.0.1")
	created, _ := d.mergeCustomProperties(cpMergeInput{
		Config:   types.MapNull(types.StringType),
		PriorAll: types.MapNull(types.StringType),
		IsCreate: true,
	})
	requireMapEquals(t, created, map[string]string{"managed-by": "terraform"})
	existing, _ := d.mergeCustomProperties(cpMergeInput{
		Config:   types.MapNull(types.StringType),
		PriorAll: types.MapNull(types.StringType),
		IsCreate: false,
	})
	if !existing.IsNull() {
		t.Fatalf("expected null merge for existing resources, got %s", existing)
	}
}

// providerDefaultsObjectType mirrors the schema of the defaults nested
// attribute for test construction.
func providerDefaultsObjectType() map[string]attr.Type {
	return map[string]attr.Type{
		"custom_properties": types.MapType{ElemType: types.StringType},
	}
}

func TestURNPrefixSetValidator(t *testing.T) {
	v := urnPrefixSetValidator{prefix: tagURNPrefix}
	cases := []struct {
		name    string
		value   types.Set
		wantErr bool
	}{
		{"valid", stringSet("urn:li:tag:terraform-managed"), false},
		{"wrong prefix", stringSet("urn:li:domain:x"), true},
		{"bare prefix", stringSet("urn:li:tag:"), true},
		{"null", types.SetNull(types.StringType), false},
		{"unknown", types.SetUnknown(types.StringType), false},
		{"empty set", stringSet(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validator.SetRequest{Path: path.Root("tags"), ConfigValue: tc.value}
			resp := &validator.SetResponse{}
			v.ValidateSet(t.Context(), req, resp)
			if got := resp.Diagnostics.HasError(); got != tc.wantErr {
				t.Fatalf("wantErr=%v, got diagnostics: %v", tc.wantErr, resp.Diagnostics)
			}
		})
	}
}

func TestSPDefaultsMapValidator(t *testing.T) {
	v := spDefaultsMapValidator{}
	mustMap := func(elems map[string]attr.Value) types.Map {
		m, diags := types.MapValue(spDefaultsElementType, elems)
		if diags.HasError() {
			t.Fatal(diags)
		}
		return m
	}
	mustSet := func(vals ...attr.Value) types.Set {
		s, diags := types.SetValue(types.StringType, vals)
		if diags.HasError() {
			t.Fatal(diags)
		}
		return s
	}
	cases := []struct {
		name    string
		value   types.Map
		wantErr bool
	}{
		{"valid", mustMap(map[string]attr.Value{
			"urn:li:structuredProperty:io.example.stack": mustSet(types.StringValue("prod")),
		}), false},
		{"bad key", mustMap(map[string]attr.Value{
			"io.example.stack": mustSet(types.StringValue("prod")),
		}), true},
		{"empty value set", mustMap(map[string]attr.Value{
			"urn:li:structuredProperty:io.example.stack": mustSet(),
		}), true},
		{"empty string value", mustMap(map[string]attr.Value{
			"urn:li:structuredProperty:io.example.stack": mustSet(types.StringValue("")),
		}), true},
		{"empty map", mustMap(map[string]attr.Value{}), true},
		{"null", types.MapNull(spDefaultsElementType), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validator.MapRequest{Path: path.Root("structured_properties"), ConfigValue: tc.value}
			resp := &validator.MapResponse{}
			v.ValidateMap(t.Context(), req, resp)
			if got := resp.Diagnostics.HasError(); got != tc.wantErr {
				t.Fatalf("wantErr=%v, got diagnostics: %v", tc.wantErr, resp.Diagnostics)
			}
		})
	}
}

func TestEnumSetValidator(t *testing.T) {
	v := enumSet(autoPropertyManagedBy, autoPropertyProviderVersion)
	cases := []struct {
		name    string
		value   types.Set
		wantErr bool
	}{
		{"valid", stringSet("managed-by", "provider-version"), false},
		{"invalid member", stringSet("managed-by", "bogus"), true},
		{"empty set is allowed", stringSet(), false},
		{"null", types.SetNull(types.StringType), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validator.SetRequest{Path: path.Root("auto_properties"), ConfigValue: tc.value}
			resp := &validator.SetResponse{}
			v.ValidateSet(t.Context(), req, resp)
			if got := resp.Diagnostics.HasError(); got != tc.wantErr {
				t.Fatalf("wantErr=%v, got diagnostics: %v", tc.wantErr, resp.Diagnostics)
			}
		})
	}
}
