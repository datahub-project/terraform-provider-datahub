// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// These tests pin the prior-state-aware read normalisation for datahub_form in
// both directions. The server materialises defaults on every read (actors =
// {owners: true}, filter condition = EQUAL, and the aspect omits type when
// unset), so Read has to suppress defaults the config never declared -- but
// only then. Each case below maps to a distinct user-visible failure:
// over-suppression hides a real out-of-band change forever; under-suppression
// produces a diff no apply can clear.

// defaultActors is the shape the server materialises for a form whose config
// omitted the actors block entirely.
func defaultActors() *datahub.FormActors {
	return &datahub.FormActors{Owners: true}
}

// mustAttrValue asserts an attr.Value to a concrete framework type, failing
// the test on a mismatch (the linter forbids unchecked type assertions).
func mustAttrValue[T attr.Value](t *testing.T, v attr.Value) T {
	t.Helper()
	typed, ok := v.(T)
	if !ok {
		t.Fatalf("value %v has unexpected type %T", v, v)
	}
	return typed
}

func TestFormActorsToModel_DefaultSuppressedWhenPriorNull(t *testing.T) {
	ctx := t.Context()
	prior := types.ObjectNull(formActorsObjectType.AttrTypes)

	got, diags := formActorsToModel(ctx, defaultActors(), prior)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	// Config omitted actors; the server default must read back as null or the
	// omitted block diffs on every plan forever.
	if !got.IsNull() {
		t.Errorf("formActorsToModel(default, prior=null) = %v, want null", got)
	}
}

func TestFormActorsToModel_DefaultKeptWhenPriorSet(t *testing.T) {
	ctx := t.Context()
	// The user declared actors = { owners = true } explicitly: prior state is a
	// non-null object that happens to equal the server default.
	prior, diags := types.ObjectValueFrom(ctx, formActorsObjectType.AttrTypes, formActorsModel{
		Owners: types.BoolValue(true),
		Users:  types.ListNull(types.StringType),
		Groups: types.ListNull(types.StringType),
	})
	if diags.HasError() {
		t.Fatalf("building prior: %v", diags)
	}

	got, d := formActorsToModel(ctx, defaultActors(), prior)
	if d.HasError() {
		t.Fatalf("diags: %v", d)
	}
	// Suppressing here would null out an attribute the config declares,
	// producing a permanent diff for anyone who writes the default explicitly.
	if got.IsNull() {
		t.Fatal("formActorsToModel(default, prior=set) = null, want object")
	}
	owners := mustAttrValue[types.Bool](t, got.Attributes()["owners"])
	if !owners.ValueBool() {
		t.Errorf("owners = %v, want true", owners)
	}
}

func TestFormActorsToModel_NonDefaultSurfacesWhenPriorNull(t *testing.T) {
	ctx := t.Context()
	prior := types.ObjectNull(formActorsObjectType.AttrTypes)

	// Someone edited the actors in the DataHub UI after the (actors-less)
	// Terraform apply. Read must surface that as drift, not suppress it as if
	// it were still the untouched default.
	got, diags := formActorsToModel(ctx, &datahub.FormActors{
		Owners: false,
		Users:  []string{"urn:li:corpuser:jdoe"},
	}, prior)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if got.IsNull() {
		t.Fatal("out-of-band actors change suppressed to null; drift would never be detected")
	}
	owners := mustAttrValue[types.Bool](t, got.Attributes()["owners"])
	if owners.ValueBool() {
		t.Errorf("owners = true, want false")
	}
	users := mustAttrValue[types.List](t, got.Attributes()["users"])
	if users.IsNull() || len(users.Elements()) != 1 {
		t.Errorf("users = %v, want 1 element", users)
	}
}

func TestFormActorsToModel_NilAspectIsNull(t *testing.T) {
	got, diags := formActorsToModel(t.Context(), nil, types.ObjectNull(formActorsObjectType.AttrTypes))
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if !got.IsNull() {
		t.Errorf("formActorsToModel(nil) = %v, want null", got)
	}
}

// TestFormActorsToModel_EmptyUsersReadsBackNull pins the documented residual
// edge: an explicit `users = []` reads back as null, because empty and absent
// are indistinguishable server-side. The docs tell users to omit the attribute
// rather than set it empty. If this test starts failing, the normalisation
// changed -- update the resource documentation (and the PR notes) in the same
// change.
func TestFormActorsToModel_EmptyUsersReadsBackNull(t *testing.T) {
	ctx := t.Context()
	emptyUsers, d := types.ListValue(types.StringType, []attr.Value{})
	if d.HasError() {
		t.Fatalf("building empty list: %v", d)
	}
	prior, diags := types.ObjectValueFrom(ctx, formActorsObjectType.AttrTypes, formActorsModel{
		Owners: types.BoolValue(true),
		Users:  emptyUsers,
		Groups: types.ListNull(types.StringType),
	})
	if diags.HasError() {
		t.Fatalf("building prior: %v", diags)
	}

	got, d2 := formActorsToModel(ctx, defaultActors(), prior)
	if d2.HasError() {
		t.Fatalf("diags: %v", d2)
	}
	if got.IsNull() {
		t.Fatal("actors = null, want object (prior state declared the block)")
	}
	users := mustAttrValue[types.List](t, got.Attributes()["users"])
	if !users.IsNull() {
		t.Errorf("users = %v, want null: empty and absent are indistinguishable server-side", users)
	}
}

func TestFormActorsToModel_GroupsRoundTrip(t *testing.T) {
	ctx := t.Context()
	got, diags := formActorsToModel(ctx, &datahub.FormActors{
		Owners: false,
		Groups: []string{"urn:li:corpGroup:governance", "urn:li:corpGroup:data-eng"},
	}, types.ObjectNull(formActorsObjectType.AttrTypes))
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if got.IsNull() {
		t.Fatal("actors = null, want object")
	}
	groups := mustAttrValue[types.List](t, got.Attributes()["groups"])
	if len(groups.Elements()) != 2 {
		t.Fatalf("groups = %v, want 2 elements", groups)
	}
	first := mustAttrValue[types.String](t, groups.Elements()[0])
	if first.ValueString() != "urn:li:corpGroup:governance" {
		t.Errorf("groups[0] = %q, want urn:li:corpGroup:governance (order must be preserved)", first.ValueString())
	}
}

func TestFormPromptsToModel_EmptyAndPriorNullIsNull(t *testing.T) {
	got, diags := formPromptsToModel(t.Context(), nil, types.ListNull(formPromptObjectType))
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if !got.IsNull() {
		t.Errorf("formPromptsToModel(empty, prior=null) = %v, want null", got)
	}
}

func TestFormPromptsToModel_EmptyKeptWhenPriorDeclared(t *testing.T) {
	ctx := t.Context()
	// The user wrote prompts = [] explicitly. Reading that back as null would
	// diff against the declared empty list on every plan.
	prior, d := types.ListValue(formPromptObjectType, []attr.Value{})
	if d.HasError() {
		t.Fatalf("building prior: %v", d)
	}
	got, diags := formPromptsToModel(ctx, nil, prior)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if got.IsNull() {
		t.Fatal("formPromptsToModel(empty, prior=[]) = null, want empty list")
	}
	if len(got.Elements()) != 0 {
		t.Errorf("elements = %d, want 0", len(got.Elements()))
	}
}

func TestFormPromptsToModel_EmptyDescriptionIsNull(t *testing.T) {
	ctx := t.Context()
	got, diags := formPromptsToModel(ctx, []datahub.FormPrompt{{
		ID:                    "p1",
		Title:                 "Classify",
		Type:                  "STRUCTURED_PROPERTY",
		StructuredPropertyURN: "urn:li:structuredProperty:x",
	}}, types.ListNull(formPromptObjectType))
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	obj := mustAttrValue[types.Object](t, got.Elements()[0])
	desc := mustAttrValue[types.String](t, obj.Attributes()["description"])
	// The aspect stores no description; a config that omitted it must not diff
	// against an empty string.
	if !desc.IsNull() {
		t.Errorf("description = %v, want null", desc)
	}
}

func TestFormAssignmentToModel_AbsentAspectIsNull(t *testing.T) {
	got, diags := formAssignmentToModel(t.Context(), nil)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if !got.IsNull() {
		t.Errorf("formAssignmentToModel(nil) = %v, want null", got)
	}
}

// TestFormAssignmentToModel_OmittedConditionDefaultsToEqual covers the aspect
// shape a defaults-dropping serialiser produces: no condition on the stored
// criterion. Reading that back as "" would permanently diff against the schema
// default EQUAL in any config.
func TestFormAssignmentToModel_OmittedConditionDefaultsToEqual(t *testing.T) {
	ctx := t.Context()
	got, diags := formAssignmentToModel(ctx, []datahub.AndFilter{{
		And: []datahub.FacetFilter{
			{Field: "platform.keyword", Values: []string{"urn:li:dataPlatform:snowflake"}},
			{Field: "domains.keyword", Values: []string{"urn:li:domain:finance"}, Condition: "CONTAIN", Negated: true},
		},
	}})
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if got.IsNull() {
		t.Fatal("assignment = null, want object")
	}
	orFilters := mustAttrValue[types.List](t, got.Attributes()["or_filters"])
	group := mustAttrValue[types.Object](t, orFilters.Elements()[0])
	ands := mustAttrValue[types.List](t, group.Attributes()["and"])
	if len(ands.Elements()) != 2 {
		t.Fatalf("and = %d criteria, want 2", len(ands.Elements()))
	}

	first := mustAttrValue[types.Object](t, ands.Elements()[0]).Attributes()
	if cond := mustAttrValue[types.String](t, first["condition"]).ValueString(); cond != "EQUAL" {
		t.Errorf("omitted condition read back as %q, want EQUAL", cond)
	}

	// A criterion that stores an explicit condition must keep it: normalising
	// everything to EQUAL would silently widen or invert a filter.
	second := mustAttrValue[types.Object](t, ands.Elements()[1]).Attributes()
	if cond := mustAttrValue[types.String](t, second["condition"]).ValueString(); cond != "CONTAIN" {
		t.Errorf("explicit condition rewritten to %q, want CONTAIN", cond)
	}
	if neg := mustAttrValue[types.Bool](t, second["negated"]).ValueBool(); !neg {
		t.Error("negated = false, want true")
	}
}
