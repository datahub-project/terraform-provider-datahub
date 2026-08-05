// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// personalScopeValidator rejects scope = "PERSONAL".
//
// A PERSONAL page module belongs to one user's own home page. Per-user
// preferences are out of scope for this provider (see the provider-scope rule
// in CLAUDE.md), and a PERSONAL module written by Terraform would belong to
// whichever account the provider's token authenticates as -- almost never what
// the author intended.
type personalScopeValidator struct{}

func (v personalScopeValidator) Description(_ context.Context) string {
	return `scope must be "GLOBAL"`
}

func (v personalScopeValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v personalScopeValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if strings.EqualFold(req.ConfigValue.ValueString(), "PERSONAL") {
		resp.Diagnostics.AddAttributeError(req.Path, "PERSONAL scope is not supported",
			"This provider manages organisation-wide configuration only. A PERSONAL page module "+
				"belongs to a single user's home page, and would be created against whichever "+
				"account the provider's token authenticates as. Use scope = \"GLOBAL\".")
		return
	}
	if !strings.EqualFold(req.ConfigValue.ValueString(), "GLOBAL") {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid scope",
			fmt.Sprintf("scope must be \"GLOBAL\", got %q.", req.ConfigValue.ValueString()))
	}
}

type pageModuleResource struct {
	client *datahub.Client
}

// pageModuleResourceModel holds the module's configuration.
//
// Params is types.Object rather than a *pageModuleParamsModel deliberately: a
// Go pointer cannot hold an unknown value, so a params block fed from a
// variable or for_each would fail conversion before the provider runs. See the
// "Unknown Values in Nested Attributes" section of
// docs/design/datahub-model-and-resource-design.md -- this class of bug has
// shipped twice in this provider.
type pageModuleResourceModel struct {
	ID           types.String `tfsdk:"id"`
	URN          types.String `tfsdk:"urn"`
	PageModuleID types.String `tfsdk:"page_module_id"`
	Name         types.String `tfsdk:"name"`
	Type         types.String `tfsdk:"type"`
	Scope        types.String `tfsdk:"scope"`
	Params       types.Object `tfsdk:"params"`
}

func NewPageModuleResource() resource.Resource {
	return &pageModuleResource{}
}

func (r *pageModuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	r.client = pd.Client
}

func (r *pageModuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_page_module"
}

func (r *pageModuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a DataHub page module -- one widget on a page laid out by " +
			"`datahub_page_template`.\n\n" +
			"Modules are separate resources rather than blocks inside a template because DataHub " +
			"addresses them independently and one module can appear on several templates. " +
			"Reference a module's `urn` from a template's `rows` and Terraform orders the two " +
			"correctly with no `depends_on`.\n\n" +
			"## Naming\n\n" +
			"`page_module_id` becomes the URN suffix (`urn:li:dataHubPageModule:<page_module_id>`) " +
			"and the provider always sends it. DataHub mints a random UUID when a module is " +
			"created without an explicit URN, which is what the DataHub UI does -- so a module " +
			"created in the UI has a UUID URN, while one created here is stable and predictable " +
			"across a destroy and re-apply. That stability is what lets a page survive a rebuild.\n\n" +
			"## Module types\n\n" +
			"`type` is passed to DataHub unvalidated by the provider, deliberately: DataHub's " +
			"module catalogue grows between releases (22 types in Cloud v2.0.3, 30 in v2.1.0), " +
			"and a list compiled into the provider would make each new type unusable until the " +
			"provider shipped a release. An unrecognised type is rejected by the server.\n\n" +
			"Types taking no parameters include `DOMAINS`, `DATA_PRODUCTS`, `OWNED_ASSETS`, " +
			"`ASSETS` and `PLATFORMS`. The five that take parameters each have a matching block " +
			"under `params`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Terraform identifier. Same value as `page_module_id`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN, `urn:li:dataHubPageModule:<page_module_id>`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"page_module_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Identifier forming the URN suffix. Changing this replaces the " +
					"module, because the URN is derived from it.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Display name shown on the module.",
			},
			"type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Module type, e.g. `LINK`, `RICH_TEXT`, `DOMAINS`, " +
					"`ASSET_COLLECTION`, `HIERARCHY`. Validated by DataHub, not by the provider.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"scope": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("GLOBAL"),
				MarkdownDescription: "Visibility scope. Only `GLOBAL` is supported; `PERSONAL` " +
					"modules are per-user preferences and out of scope for this provider.",
				Validators: []validator.String{personalScopeValidator{}},
			},
			"params": schema.SingleNestedAttribute{
				Optional: true,
				MarkdownDescription: "Type-specific configuration. Supply exactly the block " +
					"matching `type`; most module types need none.",
				Attributes: map[string]schema.Attribute{
					"link": schema.SingleNestedAttribute{
						Optional:            true,
						MarkdownDescription: "For `type = \"LINK\"`.",
						Attributes: map[string]schema.Attribute{
							"link_url":    schema.StringAttribute{Optional: true, MarkdownDescription: "Target URL."},
							"image_url":   schema.StringAttribute{Optional: true, MarkdownDescription: "Optional image shown on the card."},
							"description": schema.StringAttribute{Optional: true, MarkdownDescription: "Optional descriptive text."},
						},
					},
					"rich_text": schema.SingleNestedAttribute{
						Optional:            true,
						MarkdownDescription: "For `type = \"RICH_TEXT\"`.",
						Attributes: map[string]schema.Attribute{
							"content": schema.StringAttribute{Optional: true, MarkdownDescription: "Module body content."},
						},
					},
					"asset_collection": schema.SingleNestedAttribute{
						Optional:            true,
						MarkdownDescription: "For `type = \"ASSET_COLLECTION\"`.",
						Attributes: map[string]schema.Attribute{
							"asset_urns": schema.ListAttribute{
								Optional:            true,
								ElementType:         types.StringType,
								MarkdownDescription: "URNs of the assets to show.",
							},
							"dynamic_filter_json": schema.StringAttribute{
								Optional:            true,
								MarkdownDescription: "Raw DataHub filter JSON selecting assets dynamically.",
							},
						},
					},
					"hierarchy_view": schema.SingleNestedAttribute{
						Optional:            true,
						MarkdownDescription: "For `type = \"HIERARCHY\"`.",
						Attributes: map[string]schema.Attribute{
							"asset_urns": schema.ListAttribute{
								Optional:            true,
								ElementType:         types.StringType,
								MarkdownDescription: "Root URNs of the hierarchy, e.g. domain or glossary node URNs.",
							},
							"show_related_entities": schema.BoolAttribute{
								Optional:            true,
								MarkdownDescription: "Whether to include related entities beneath each root.",
							},
							"related_entities_filter_json": schema.StringAttribute{
								Optional:            true,
								MarkdownDescription: "Raw DataHub filter JSON narrowing the related entities.",
							},
						},
					},
					"agent_card": schema.SingleNestedAttribute{
						Optional:            true,
						MarkdownDescription: "For `type = \"AGENT_CARD\"`.",
						Attributes: map[string]schema.Attribute{
							"agent_urn":    schema.StringAttribute{Optional: true, MarkdownDescription: "URN of the agent to surface."},
							"display_mode": schema.StringAttribute{Optional: true, MarkdownDescription: "Presentation mode for the card."},
						},
					},
				},
			},
		},
	}
}

// The attribute-type maps below mirror the params schema for object
// conversion. Each sub-object's map is its own function so that conversion code
// can reach it directly, rather than type-asserting it back out of the parent
// map -- an assertion the linter rightly rejects as unchecked.

func linkAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"link_url":    types.StringType,
		"image_url":   types.StringType,
		"description": types.StringType,
	}
}

func richTextAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{"content": types.StringType}
}

func assetCollectionAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"asset_urns":          types.ListType{ElemType: types.StringType},
		"dynamic_filter_json": types.StringType,
	}
}

func hierarchyViewAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"asset_urns":                   types.ListType{ElemType: types.StringType},
		"show_related_entities":        types.BoolType,
		"related_entities_filter_json": types.StringType,
	}
}

func agentCardAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"agent_urn":    types.StringType,
		"display_mode": types.StringType,
	}
}

func pageModuleParamsAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"link":             types.ObjectType{AttrTypes: linkAttrTypes()},
		"rich_text":        types.ObjectType{AttrTypes: richTextAttrTypes()},
		"asset_collection": types.ObjectType{AttrTypes: assetCollectionAttrTypes()},
		"hierarchy_view":   types.ObjectType{AttrTypes: hierarchyViewAttrTypes()},
		"agent_card":       types.ObjectType{AttrTypes: agentCardAttrTypes()},
	}
}

func (r *pageModuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan pageModuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, diags := pageModuleInputFromModel(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn, err := r.client.UpsertPageModule(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create page module", err.Error())
		return
	}

	plan.ID = types.StringValue(plan.PageModuleID.ValueString())
	plan.URN = types.StringValue(urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *pageModuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state pageModuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	module, err := r.client.GetPageModuleByURN(ctx, state.URN.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read page module", err.Error())
		return
	}
	if module == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	diags := applyPageModuleToModel(ctx, module, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *pageModuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan pageModuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, diags := pageModuleInputFromModel(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn, err := r.client.UpsertPageModule(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update page module", err.Error())
		return
	}

	plan.ID = types.StringValue(plan.PageModuleID.ValueString())
	plan.URN = types.StringValue(urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *pageModuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state pageModuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeletePageModule(ctx, state.URN.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to delete page module", err.Error())
	}
}

func (r *pageModuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := strings.TrimSpace(req.ID)
	if id == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Supply a page module id or its full URN.")
		return
	}

	urn := id
	if !strings.HasPrefix(urn, datahub.PageModuleURNPrefix) {
		urn = datahub.PageModuleURN(id)
	}

	module, err := r.client.GetPageModuleByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("Unable to import page module", err.Error())
		return
	}
	if module == nil {
		resp.Diagnostics.AddError("Page module not found",
			fmt.Sprintf("No page module exists at %s.", urn))
		return
	}

	state := pageModuleResourceModel{
		ID:           types.StringValue(module.ID),
		URN:          types.StringValue(module.URN),
		PageModuleID: types.StringValue(module.ID),
	}
	diags := applyPageModuleToModel(ctx, module, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
