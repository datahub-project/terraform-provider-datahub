// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

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

	if m.Actors != nil {
		users, d := setToStrings(ctx, m.Actors.Users)
		diags.Append(d...)
		groups, d := setToStrings(ctx, m.Actors.Groups)
		diags.Append(d...)
		ownerTypes, d := setToStrings(ctx, m.Actors.ResourceOwnersTypes)
		diags.Append(d...)
		in.Actors = datahub.PolicyActors{
			Users:               users,
			Groups:              groups,
			AllUsers:            m.Actors.AllUsers.ValueBool(),
			AllGroups:           m.Actors.AllGroups.ValueBool(),
			ResourceOwners:      m.Actors.ResourceOwners.ValueBool(),
			ResourceOwnersTypes: ownerTypes,
		}
	}

	if m.Resources != nil {
		resources, d := setToStrings(ctx, m.Resources.Resources)
		diags.Append(d...)
		in.Resources = &datahub.PolicyResources{
			Type:         m.Resources.Type.ValueString(),
			Resources:    resources,
			AllResources: m.Resources.AllResources.ValueBool(),
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
	m.Actors = &policyActorsModel{
		Users:               users,
		Groups:              groups,
		AllUsers:            types.BoolValue(p.Actors.AllUsers),
		AllGroups:           types.BoolValue(p.Actors.AllGroups),
		ResourceOwners:      types.BoolValue(p.Actors.ResourceOwners),
		ResourceOwnersTypes: ownerTypes,
	}

	if p.Resources != nil {
		resources, d := stringsToSet(ctx, p.Resources.Resources, true)
		diags.Append(d...)
		m.Resources = &policyResourcesModel{
			Type:         nullIfEmpty(p.Resources.Type),
			Resources:    resources,
			AllResources: types.BoolValue(p.Resources.AllResources),
		}
	} else {
		m.Resources = nil
	}

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
