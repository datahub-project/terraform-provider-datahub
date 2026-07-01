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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

const domainURNPrefix = "urn:li:domain:"

var (
	_ resource.Resource                = &domainResource{}
	_ resource.ResourceWithConfigure   = &domainResource{}
	_ resource.ResourceWithImportState = &domainResource{}
)

type domainResource struct {
	client *datahub.Client
}

type domainResourceModel struct {
	ID               types.String `tfsdk:"id"`
	URN              types.String `tfsdk:"urn"`
	DomainID         types.String `tfsdk:"domain_id"`
	Name             types.String `tfsdk:"name"`
	Description      types.String `tfsdk:"description"`
	ParentDomain     types.String `tfsdk:"parent_domain"`
	CustomProperties types.Map    `tfsdk:"custom_properties"`
}

func NewDomainResource() resource.Resource {
	return &domainResource{}
}

func (r *domainResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*datahub.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *datahub.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	r.client = client
}

func (r *domainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (r *domainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a DataHub domain.\n\n" +
			"Domains organize data assets into a logical business hierarchy. They can be nested " +
			"to any depth via `parent_domain`, forming a tree (e.g. Business Area -> " +
			"Business Domain -> Service Domain).\n\n" +
			"## Ordering\n\n" +
			"Set `parent_domain` to the `.urn` attribute of another `datahub_domain` resource " +
			"(rather than a raw URN string) so Terraform's dependency graph creates parents " +
			"before children and destroys children before parents. DataHub hard-deletes domains " +
			"and refuses deletion if any child domains exist; correct ordering is required for " +
			"`terraform destroy` to succeed.\n\n" +
			"## Naming\n\n" +
			"`domain_id` becomes the URN suffix (`urn:li:domain:<domain_id>`). Supplying an " +
			"explicit, deterministic id avoids the random UUID that the DataHub UI assigns, and " +
			"keeps the URN stable and predictable.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this domain (e.g., `urn:li:domain:marketing`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"domain_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Unique identifier for the domain. Becomes the URN suffix (`urn:li:domain:<domain_id>`). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable display name for the domain, shown throughout the DataHub UI.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the domain's scope and purpose.",
			},
			"parent_domain": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Full URN of the parent domain (e.g., `urn:li:domain:finance`). " +
					"Set to `datahub_domain.<name>.urn` (not a raw string) so Terraform's dependency " +
					"graph orders creation and destruction correctly. Omit for a root domain. " +
					"Changing this value reparents the domain in place without forcing replacement.",
			},
			"custom_properties": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Arbitrary key-value metadata attached to the domain (the " +
					"`customProperties` field of the `domainProperties` aspect). Terraform owns the " +
					"complete map: keys added outside Terraform are removed on the next apply. Omit " +
					"for none.",
			},
		},
	}
}

func (r *domainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan domainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	customProps, d := mapValToStringMap(ctx, plan.CustomProperties)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn, err := r.client.CreateDomain(ctx, datahub.CreateDomainInput{
		ID:           plan.DomainID.ValueString(),
		Name:         plan.Name.ValueString(),
		Description:  strVal(plan.Description),
		ParentDomain: strVal(plan.ParentDomain),
	})
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	// custom_properties is not carried by the GraphQL createDomain mutation, so
	// write it via the OpenAPI v3 entity endpoint (carrying name/description/
	// parentDomain to avoid clobbering what createDomain just set).
	if len(customProps) > 0 {
		if err := r.client.SetDomainProperties(ctx, urn, plan.Name.ValueString(), strVal(plan.Description), strVal(plan.ParentDomain), customProps); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	plan.ID = types.StringValue(urn)
	plan.URN = types.StringValue(urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *domainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state domainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}

	domain, err := r.client.GetDomainByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if domain == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	applyDomainToModel(domain, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *domainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan, state domainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()

	if plan.Name.ValueString() != state.Name.ValueString() {
		if err := r.client.UpdateEntityName(ctx, urn, plan.Name.ValueString()); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	if strVal(plan.Description) != strVal(state.Description) {
		if err := r.client.UpdateEntityDescription(ctx, urn, strVal(plan.Description)); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	if strVal(plan.ParentDomain) != strVal(state.ParentDomain) {
		if err := r.client.MoveDomain(ctx, urn, strVal(plan.ParentDomain)); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	// Write custom_properties via OpenAPI v3 when it changed (including clearing
	// it: an empty map overwrites a previously-set value). Pass the plan's
	// name/description/parentDomain so the domainProperties aspect write does not
	// clobber the values the GraphQL mutations above just applied.
	if !plan.CustomProperties.Equal(state.CustomProperties) {
		customProps, d := mapValToStringMap(ctx, plan.CustomProperties)
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		if err := r.client.SetDomainProperties(ctx, urn, plan.Name.ValueString(), strVal(plan.Description), strVal(plan.ParentDomain), customProps); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	plan.ID = state.ID
	plan.URN = state.URN
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *domainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state domainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}
	if urn == "" {
		resp.State.RemoveResource(ctx)
		return
	}

	if err := r.client.DeleteDomain(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *domainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub domain URN (e.g., urn:li:domain:marketing) or a bare domain ID.")
		return
	}

	var domainID, urn string
	if strings.HasPrefix(raw, domainURNPrefix) {
		urn = raw
		domainID = strings.TrimPrefix(raw, domainURNPrefix)
	} else {
		domainID = raw
		urn = domainURNPrefix + domainID
	}
	if domainID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub domain URN (e.g., urn:li:domain:marketing) or a bare domain ID.")
		return
	}

	domain, err := r.client.GetDomainByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if domain == nil {
		resp.Diagnostics.AddError(
			"Domain not found",
			fmt.Sprintf("No domain with URN %q was found in DataHub. Verify the ID or URN and retry.", urn),
		)
		return
	}

	state := domainResourceModel{
		ID:       types.StringValue(domain.URN),
		URN:      types.StringValue(domain.URN),
		DomainID: types.StringValue(domain.ID),
	}
	applyDomainToModel(domain, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// applyDomainToModel maps a read Domain onto the model, normalising empty
// optional fields to null so they do not show spurious drift.
func applyDomainToModel(domain *datahub.Domain, m *domainResourceModel) {
	m.URN = types.StringValue(domain.URN)
	m.ID = types.StringValue(domain.URN)
	m.Name = types.StringValue(domain.Name)
	m.Description = nullIfEmpty(domain.Description)
	m.ParentDomain = nullIfEmpty(domain.ParentDomain)
	m.CustomProperties = stringMapToTfMap(domain.CustomProperties)
}

// stringMapToTfMap converts a Go string map to a types.Map, normalising an
// empty or nil map to null so an unset custom_properties does not show drift.
func stringMapToTfMap(m map[string]string) types.Map {
	if len(m) == 0 {
		return types.MapNull(types.StringType)
	}
	elems := make(map[string]attr.Value, len(m))
	for k, v := range m {
		elems[k] = types.StringValue(v)
	}
	mv, diags := types.MapValue(types.StringType, elems)
	if diags.HasError() {
		return types.MapNull(types.StringType)
	}
	return mv
}
