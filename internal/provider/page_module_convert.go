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

// linkParamsModel and friends exist only as conversion targets for
// types.Object. The resource model itself holds types.Object so that an
// unknown value -- a params block fed from a variable or for_each -- survives
// as far as the provider. These structs are populated only after their object
// has been confirmed known and non-null.
type linkParamsModel struct {
	LinkURL     types.String `tfsdk:"link_url"`
	ImageURL    types.String `tfsdk:"image_url"`
	Description types.String `tfsdk:"description"`
}

type richTextParamsModel struct {
	Content types.String `tfsdk:"content"`
}

type assetCollectionParamsModel struct {
	AssetURNs         types.List   `tfsdk:"asset_urns"`
	DynamicFilterJSON types.String `tfsdk:"dynamic_filter_json"`
}

type hierarchyViewParamsModel struct {
	AssetURNs                 types.List   `tfsdk:"asset_urns"`
	ShowRelatedEntities       types.Bool   `tfsdk:"show_related_entities"`
	RelatedEntitiesFilterJSON types.String `tfsdk:"related_entities_filter_json"`
}

type agentCardParamsModel struct {
	AgentURN    types.String `tfsdk:"agent_urn"`
	DisplayMode types.String `tfsdk:"display_mode"`
}

// nestedObject pulls one sub-object out of the params object, reporting whether
// it is present and usable. An unknown object is treated as absent: its value
// is not yet resolvable, so there is nothing to send.
func nestedObject(params types.Object, key string) (types.Object, bool) {
	attrs := params.Attributes()
	raw, ok := attrs[key]
	if !ok || raw == nil {
		return types.Object{}, false
	}
	obj, ok := raw.(types.Object)
	if !ok || obj.IsNull() || obj.IsUnknown() {
		return types.Object{}, false
	}
	return obj, true
}

// stringList converts a types.List of strings, treating null and unknown alike
// as "nothing to send".
func stringList(ctx context.Context, l types.List) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics
	if l.IsNull() || l.IsUnknown() {
		return nil, diags
	}
	var out []string
	diags.Append(l.ElementsAs(ctx, &out, false)...)
	return out, diags
}

// pageModuleInputFromModel renders the Terraform model into the client input.
func pageModuleInputFromModel(ctx context.Context, m *pageModuleResourceModel) (datahub.UpsertPageModuleInput, diag.Diagnostics) {
	var diags diag.Diagnostics

	in := datahub.UpsertPageModuleInput{
		ID:   m.PageModuleID.ValueString(),
		Name: m.Name.ValueString(),
		Type: m.Type.ValueString(),
	}
	// ValueString() returns "" for unknown, so scope falls back to the schema
	// default rather than being sent empty.
	if !m.Scope.IsNull() && !m.Scope.IsUnknown() {
		in.Scope = m.Scope.ValueString()
	}
	if in.Scope == "" {
		in.Scope = "GLOBAL"
	}

	if m.Params.IsNull() || m.Params.IsUnknown() {
		return in, diags
	}

	if obj, ok := nestedObject(m.Params, "link"); ok {
		var lm linkParamsModel
		diags.Append(obj.As(ctx, &lm, basetypes.ObjectAsOptions{})...)
		if !diags.HasError() {
			in.Params.Link = &datahub.LinkModuleParams{
				LinkURL:     lm.LinkURL.ValueString(),
				ImageURL:    lm.ImageURL.ValueString(),
				Description: lm.Description.ValueString(),
			}
		}
	}
	if obj, ok := nestedObject(m.Params, "rich_text"); ok {
		var rm richTextParamsModel
		diags.Append(obj.As(ctx, &rm, basetypes.ObjectAsOptions{})...)
		if !diags.HasError() {
			in.Params.RichText = &datahub.RichTextModuleParams{Content: rm.Content.ValueString()}
		}
	}
	if obj, ok := nestedObject(m.Params, "asset_collection"); ok {
		var am assetCollectionParamsModel
		diags.Append(obj.As(ctx, &am, basetypes.ObjectAsOptions{})...)
		if !diags.HasError() {
			urns, d := stringList(ctx, am.AssetURNs)
			diags.Append(d...)
			in.Params.AssetCollection = &datahub.AssetCollectionModuleParams{
				AssetURNs:         urns,
				DynamicFilterJSON: am.DynamicFilterJSON.ValueString(),
			}
		}
	}
	if obj, ok := nestedObject(m.Params, "hierarchy_view"); ok {
		var hm hierarchyViewParamsModel
		diags.Append(obj.As(ctx, &hm, basetypes.ObjectAsOptions{})...)
		if !diags.HasError() {
			urns, d := stringList(ctx, hm.AssetURNs)
			diags.Append(d...)
			p := &datahub.HierarchyViewModuleParams{
				AssetURNs:                 urns,
				RelatedEntitiesFilterJSON: hm.RelatedEntitiesFilterJSON.ValueString(),
			}
			if !hm.ShowRelatedEntities.IsNull() && !hm.ShowRelatedEntities.IsUnknown() {
				v := hm.ShowRelatedEntities.ValueBool()
				p.ShowRelatedEntities = &v
			}
			in.Params.HierarchyView = p
		}
	}
	if obj, ok := nestedObject(m.Params, "agent_card"); ok {
		var cm agentCardParamsModel
		diags.Append(obj.As(ctx, &cm, basetypes.ObjectAsOptions{})...)
		if !diags.HasError() {
			in.Params.AgentCard = &datahub.AgentCardModuleParams{
				AgentURN:    cm.AgentURN.ValueString(),
				DisplayMode: cm.DisplayMode.ValueString(),
			}
		}
	}

	return in, diags
}

// applyPageModuleToModel writes a server read back into the Terraform model.
func applyPageModuleToModel(ctx context.Context, module *datahub.PageModule, m *pageModuleResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	m.ID = types.StringValue(module.ID)
	m.URN = types.StringValue(module.URN)
	m.PageModuleID = types.StringValue(module.ID)
	m.Name = types.StringValue(module.Name)
	m.Type = types.StringValue(module.Type)
	if module.Scope != "" {
		m.Scope = types.StringValue(module.Scope)
	}

	attrTypes := pageModuleParamsAttrTypes()
	values := map[string]attr.Value{
		"link":             types.ObjectNull(linkAttrTypes()),
		"rich_text":        types.ObjectNull(richTextAttrTypes()),
		"asset_collection": types.ObjectNull(assetCollectionAttrTypes()),
		"hierarchy_view":   types.ObjectNull(hierarchyViewAttrTypes()),
		"agent_card":       types.ObjectNull(agentCardAttrTypes()),
	}

	p := module.Params
	populated := false

	if p.Link != nil {
		obj, d := types.ObjectValue(linkAttrTypes(), map[string]attr.Value{
			"link_url":    optionalString(p.Link.LinkURL),
			"image_url":   optionalString(p.Link.ImageURL),
			"description": optionalString(p.Link.Description),
		})
		diags.Append(d...)
		values["link"] = obj
		populated = true
	}
	if p.RichText != nil {
		obj, d := types.ObjectValue(richTextAttrTypes(), map[string]attr.Value{
			"content": optionalString(p.RichText.Content),
		})
		diags.Append(d...)
		values["rich_text"] = obj
		populated = true
	}
	if p.AssetCollection != nil {
		urns, d := optionalStringList(ctx, p.AssetCollection.AssetURNs)
		diags.Append(d...)
		obj, d2 := types.ObjectValue(assetCollectionAttrTypes(), map[string]attr.Value{
			"asset_urns":          urns,
			"dynamic_filter_json": optionalString(p.AssetCollection.DynamicFilterJSON),
		})
		diags.Append(d2...)
		values["asset_collection"] = obj
		populated = true
	}
	if p.HierarchyView != nil {
		urns, d := optionalStringList(ctx, p.HierarchyView.AssetURNs)
		diags.Append(d...)
		show := types.BoolNull()
		if p.HierarchyView.ShowRelatedEntities != nil {
			show = types.BoolValue(*p.HierarchyView.ShowRelatedEntities)
		}
		obj, d2 := types.ObjectValue(hierarchyViewAttrTypes(), map[string]attr.Value{
			"asset_urns":                   urns,
			"show_related_entities":        show,
			"related_entities_filter_json": optionalString(p.HierarchyView.RelatedEntitiesFilterJSON),
		})
		diags.Append(d2...)
		values["hierarchy_view"] = obj
		populated = true
	}
	if p.AgentCard != nil {
		obj, d := types.ObjectValue(agentCardAttrTypes(), map[string]attr.Value{
			"agent_urn":    optionalString(p.AgentCard.AgentURN),
			"display_mode": optionalString(p.AgentCard.DisplayMode),
		})
		diags.Append(d...)
		values["agent_card"] = obj
		populated = true
	}

	if !populated {
		m.Params = types.ObjectNull(attrTypes)
		return diags
	}

	obj, d := types.ObjectValue(attrTypes, values)
	diags.Append(d...)
	m.Params = obj
	return diags
}

// optionalString maps the server's empty string to null, so an unset field does
// not read back as a configured empty value and produce a permanent diff.
func optionalString(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

func optionalStringList(ctx context.Context, in []string) (types.List, diag.Diagnostics) {
	if len(in) == 0 {
		return types.ListNull(types.StringType), nil
	}
	return types.ListValueFrom(ctx, types.StringType, in)
}
