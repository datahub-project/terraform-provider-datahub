// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// The nested blocks are modelled as types.Object / types.List rather than Go
// pointers and slices because a Go pointer or slice has no representation for
// an unknown value.
//
// Terraform plans an Optional+Computed attribute as unknown whenever it cannot
// resolve the schema default itself, which is exactly what happens when a
// nested block is fed from a variable, a for_each, or any other non-literal
// expression whose object type omits that attribute. actors carries three such
// booleans, resources carries all_resources, and each criterion carries
// condition. Converting the config into Go-native fields then failed before the
// provider ran at all:
//
//	Received unknown value, however the target type cannot handle unknown
//	values. Path: resources.filter.criteria
//	Target Type: []provider.policyMatchCriterionModel
//
// The framework types hold unknown, so conversion succeeds and the provider
// decides what to do. The Go structs below are kept as the targets of As() and
// ObjectValueFrom, so the read and write paths stay readable.

func policyActorsAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"users":                 types.SetType{ElemType: types.StringType},
		"groups":                types.SetType{ElemType: types.StringType},
		"all_users":             types.BoolType,
		"all_groups":            types.BoolType,
		"resource_owners":       types.BoolType,
		"resource_owners_types": types.SetType{ElemType: types.StringType},
	}
}

func policyMatchCriterionAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"field":     types.StringType,
		"values":    types.ListType{ElemType: types.StringType},
		"condition": types.StringType,
	}
}

func policyMatchFilterAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"criteria": types.ListType{
			ElemType: types.ObjectType{AttrTypes: policyMatchCriterionAttrTypes()},
		},
	}
}

func policyResourcesAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"type":          types.StringType,
		"resources":     types.SetType{ElemType: types.StringType},
		"all_resources": types.BoolType,
		"filter":        types.ObjectType{AttrTypes: policyMatchFilterAttrTypes()},
	}
}

// objectAsOptions treats null and unknown descendants as zero values rather than
// erroring. The caller has already established that the object itself is known;
// an individual attribute may still be unknown (an unresolved Optional+Computed
// default), and the zero value is the right reading of that for a write input,
// since the server applies its own default for anything not sent.
var objectAsOptions = basetypes.ObjectAsOptions{
	UnhandledNullAsEmpty:    true,
	UnhandledUnknownAsEmpty: true,
}

// policyActorsFromObject decodes the actors object. A null or unknown object
// yields ok=false, meaning "nothing to send".
func policyActorsFromObject(ctx context.Context, o types.Object) (policyActorsModel, bool, diag.Diagnostics) {
	var out policyActorsModel
	if o.IsNull() || o.IsUnknown() {
		return out, false, nil
	}
	diags := o.As(ctx, &out, objectAsOptions)
	return out, !diags.HasError(), diags
}

// policyResourcesFromObject decodes the resources object, same convention.
func policyResourcesFromObject(ctx context.Context, o types.Object) (policyResourcesModel, bool, diag.Diagnostics) {
	var out policyResourcesModel
	if o.IsNull() || o.IsUnknown() {
		return out, false, nil
	}
	diags := o.As(ctx, &out, objectAsOptions)
	return out, !diags.HasError(), diags
}

// policyCriteriaFromList decodes a criteria list. A null or unknown list yields
// ok=false; individual unknown elements cannot be decoded either, so the same
// applies to a list whose elements are not yet resolved.
func policyCriteriaFromList(ctx context.Context, l types.List) ([]policyMatchCriterionModel, bool, diag.Diagnostics) {
	if l.IsNull() || l.IsUnknown() {
		return nil, false, nil
	}
	out := make([]policyMatchCriterionModel, 0, len(l.Elements()))
	diags := l.ElementsAs(ctx, &out, false)
	return out, !diags.HasError(), diags
}

// policyInputFromModel builds the client write-shape from the resource model.
// The full privilege/actor/resource state is always sent (aspect-list ownership).
func policyInputFromModel(ctx context.Context, m *policyResourceModel) (datahub.PolicyInput, diag.Diagnostics) {
	var diags diag.Diagnostics

	privileges, d := setToStrings(ctx, m.Privileges)
	diags.Append(d...)

	in := datahub.PolicyInput{
		Type:        m.Type.ValueString(),
		Name:        m.Name.ValueString(),
		State:       m.State.ValueString(),
		Description: m.Description.ValueString(),
		Privileges:  privileges,
	}

	actors, ok, d := policyActorsFromObject(ctx, m.Actors)
	diags.Append(d...)
	if ok {
		users, d := setToStrings(ctx, actors.Users)
		diags.Append(d...)
		groups, d := setToStrings(ctx, actors.Groups)
		diags.Append(d...)
		ownerTypes, d := setToStrings(ctx, actors.ResourceOwnersTypes)
		diags.Append(d...)
		in.Actors = datahub.PolicyActors{
			Users:               users,
			Groups:              groups,
			AllUsers:            actors.AllUsers.ValueBool(),
			AllGroups:           actors.AllGroups.ValueBool(),
			ResourceOwners:      actors.ResourceOwners.ValueBool(),
			ResourceOwnersTypes: ownerTypes,
		}
	}

	res, ok, d := policyResourcesFromObject(ctx, m.Resources)
	diags.Append(d...)
	if ok {
		resources, d := setToStrings(ctx, res.Resources)
		diags.Append(d...)
		in.Resources = &datahub.PolicyResources{
			Type:         res.Type.ValueString(),
			Resources:    resources,
			AllResources: res.AllResources.ValueBool(),
		}

		if !res.Filter.IsNull() && !res.Filter.IsUnknown() {
			var filter policyMatchFilterModel
			diags.Append(res.Filter.As(ctx, &filter, objectAsOptions)...)

			list, ok, d := policyCriteriaFromList(ctx, filter.Criteria)
			diags.Append(d...)
			if ok {
				criteria := make([]datahub.PolicyMatchCriterion, 0, len(list))
				for _, c := range list {
					values, d := listToStrings(ctx, c.Values)
					diags.Append(d...)
					criteria = append(criteria, datahub.PolicyMatchCriterion{
						Field:     c.Field.ValueString(),
						Values:    values,
						Condition: c.Condition.ValueString(),
					})
				}
				in.Resources.Filter = &datahub.PolicyMatchFilter{Criteria: criteria}
			}
		}
	}

	return in, diags
}

// applyPolicyToModel maps a read Policy onto the resource model. Optional string
// sets that come back empty are stored as null to match omitted configuration.
func applyPolicyToModel(ctx context.Context, p *datahub.Policy, m *policyResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	m.URN = types.StringValue(p.URN)
	m.ID = types.StringValue(p.URN)
	m.Name = types.StringValue(p.Name)
	m.Type = types.StringValue(p.Type)
	m.State = types.StringValue(p.State)
	m.Description = types.StringValue(p.Description)

	privileges, d := stringsToSet(ctx, p.Privileges, false)
	diags.Append(d...)
	m.Privileges = privileges

	users, d := stringsToSet(ctx, p.Actors.Users, true)
	diags.Append(d...)
	groups, d := stringsToSet(ctx, p.Actors.Groups, true)
	diags.Append(d...)
	ownerTypes, d := stringsToSet(ctx, p.Actors.ResourceOwnersTypes, true)
	diags.Append(d...)

	actorsObj, d := types.ObjectValueFrom(ctx, policyActorsAttrTypes(), policyActorsModel{
		Users:               users,
		Groups:              groups,
		AllUsers:            types.BoolValue(p.Actors.AllUsers),
		AllGroups:           types.BoolValue(p.Actors.AllGroups),
		ResourceOwners:      types.BoolValue(p.Actors.ResourceOwners),
		ResourceOwnersTypes: ownerTypes,
	})
	diags.Append(d...)
	m.Actors = actorsObj

	if p.Resources == nil {
		m.Resources = types.ObjectNull(policyResourcesAttrTypes())
		return diags
	}

	resources, d := stringsToSet(ctx, p.Resources.Resources, true)
	diags.Append(d...)

	filterObj := types.ObjectNull(policyMatchFilterAttrTypes())
	if f := p.Resources.Filter; f != nil {
		criteria := make([]policyMatchCriterionModel, 0, len(f.Criteria))
		for _, c := range f.Criteria {
			values, d := stringsToList(ctx, c.Values)
			diags.Append(d...)
			criteria = append(criteria, policyMatchCriterionModel{
				Field:     types.StringValue(c.Field),
				Values:    values,
				Condition: types.StringValue(c.Condition),
			})
		}
		criteriaList, d := types.ListValueFrom(ctx,
			types.ObjectType{AttrTypes: policyMatchCriterionAttrTypes()}, criteria)
		diags.Append(d...)

		filterObj, d = types.ObjectValueFrom(ctx, policyMatchFilterAttrTypes(),
			policyMatchFilterModel{Criteria: criteriaList})
		diags.Append(d...)
	}

	resourcesObj, d := types.ObjectValueFrom(ctx, policyResourcesAttrTypes(), policyResourcesModel{
		Type:         nullIfEmpty(p.Resources.Type),
		Resources:    resources,
		AllResources: types.BoolValue(p.Resources.AllResources),
		Filter:       filterObj,
	})
	diags.Append(d...)
	m.Resources = resourcesObj

	return diags
}

// setToStrings converts a types.Set of strings to a []string. A null or unknown
// set yields nil.
func setToStrings(ctx context.Context, s types.Set) ([]string, diag.Diagnostics) {
	if s.IsNull() || s.IsUnknown() {
		return nil, nil
	}
	out := make([]string, 0, len(s.Elements()))
	diags := s.ElementsAs(ctx, &out, false)
	return out, diags
}

// stringsToSet converts a []string to a types.Set. When nullIfEmpty is true an
// empty input yields a null set (to match omitted optional configuration);
// otherwise it yields an empty set.
func stringsToSet(ctx context.Context, in []string, nullIfEmptyInput bool) (types.Set, diag.Diagnostics) {
	if len(in) == 0 && nullIfEmptyInput {
		return types.SetNull(types.StringType), nil
	}
	return types.SetValueFrom(ctx, types.StringType, in)
}
