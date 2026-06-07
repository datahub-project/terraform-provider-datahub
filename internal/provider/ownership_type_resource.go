// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// systemPrefixValidator rejects type_id values that begin with the reserved
// __system__ prefix. System ownership types (e.g. Technical Owner, Business
// Owner) are bootstrapped by DataHub and cannot be managed or deleted via the
// provider.
type systemPrefixValidator struct{}

func (v systemPrefixValidator) Description(_ context.Context) string {
	return "must not begin with __system__ (reserved for built-in DataHub ownership types)"
}

func (v systemPrefixValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v systemPrefixValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if strings.HasPrefix(req.ConfigValue.ValueString(), "__system__") {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Reserved type_id prefix",
			fmt.Sprintf("%q begins with __system__, which is reserved for built-in DataHub ownership types (Technical Owner, Business Owner, etc.). "+
				"These types cannot be managed by Terraform. To reference a built-in type, use the datahub_ownership_type data source instead.",
				req.ConfigValue.ValueString(),
			),
		)
	}
}

const ownershipTypeURNPrefix = "urn:li:ownershipType:"

var (
	_ resource.Resource                = &ownershipTypeResource{}
	_ resource.ResourceWithConfigure   = &ownershipTypeResource{}
	_ resource.ResourceWithImportState = &ownershipTypeResource{}
)

type ownershipTypeResource struct {
	client *datahub.Client
}

type ownershipTypeResourceModel struct {
	ID          types.String `tfsdk:"id"`
	URN         types.String `tfsdk:"urn"`
	TypeID      types.String `tfsdk:"type_id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

func NewOwnershipTypeResource() resource.Resource {
	return &ownershipTypeResource{}
}

func (r *ownershipTypeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ownershipTypeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ownership_type"
}

func (r *ownershipTypeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a custom DataHub ownership type.\n\n" +
			"Ownership types define named roles for asset owners, such as \"Data Steward\", " +
			"\"Producer\", or \"Data Quality Lead\". Once created, they can be assigned to assets " +
			"alongside owner identities in DataHub's ownership model.\n\n" +
			"DataHub ships four built-in system ownership types " +
			"(`Technical Owner`, `Business Owner`, `Data Steward`, `None`). These have URN ids " +
			"prefixed with `__system__` and cannot be managed or deleted by this resource. To " +
			"look up a built-in type's URN, use the `datahub_ownership_type` data source.\n\n" +
			"## Naming\n\n" +
			"`type_id` becomes the URN suffix (`urn:li:ownershipType:<type_id>`). Supply a " +
			"stable human-readable slug (e.g. `data_quality_lead`) rather than a UUID. This " +
			"matches the DataHub Python SDK convention and ensures the URN is predictable and " +
			"importable across environments.\n\n" +
			"## Write path\n\n" +
			"Create and update write the `ownershipTypeInfo` aspect directly via the DataHub " +
			"OpenAPI v3 endpoint with the user-supplied `type_id`. The GraphQL " +
			"`createOwnershipType` mutation is not used because it generates a server-side " +
			"random UUID for the id, which is incompatible with Terraform's declarative model.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this ownership type (e.g. `urn:li:ownershipType:data_quality_lead`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"type_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Unique identifier for the ownership type. Becomes the URN suffix " +
					"(`urn:li:ownershipType:<type_id>`). Use a stable human-readable slug " +
					"(e.g. `data_quality_lead`). Must not begin with `__system__` (reserved for " +
					"built-in types). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					systemPrefixValidator{},
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable display name for the ownership type, shown throughout the DataHub UI.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the ownership type's purpose and intended usage.",
			},
		},
	}
}

func (r *ownershipTypeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan ownershipTypeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	typeID := plan.TypeID.ValueString()
	urn := ownershipTypeURNPrefix + typeID

	if err := r.client.WriteOwnershipTypeInfo(ctx, urn, plan.Name.ValueString(), strVal(plan.Description)); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = types.StringValue(urn)
	plan.URN = types.StringValue(urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ownershipTypeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state ownershipTypeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}

	ot, err := r.client.GetOwnershipTypeByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if ot == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	applyOwnershipTypeToModel(ot, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ownershipTypeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan, state ownershipTypeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()

	if err := r.client.WriteOwnershipTypeInfo(ctx, urn, plan.Name.ValueString(), strVal(plan.Description)); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = state.ID
	plan.URN = state.URN
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ownershipTypeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state ownershipTypeResourceModel
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

	if err := r.client.DeleteOwnershipType(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *ownershipTypeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub ownership type URN (e.g. urn:li:ownershipType:data_quality_lead) or a bare type ID.")
		return
	}

	var typeID, urn string
	if strings.HasPrefix(raw, ownershipTypeURNPrefix) {
		urn = raw
		typeID = strings.TrimPrefix(raw, ownershipTypeURNPrefix)
	} else {
		typeID = raw
		urn = ownershipTypeURNPrefix + typeID
	}
	if typeID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub ownership type URN (e.g. urn:li:ownershipType:data_quality_lead) or a bare type ID.")
		return
	}

	ot, err := r.client.GetOwnershipTypeByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if ot == nil {
		resp.Diagnostics.AddError(
			"Ownership type not found",
			fmt.Sprintf("No ownership type with URN %q was found in DataHub. Verify the ID or URN and retry.", urn),
		)
		return
	}

	state := ownershipTypeResourceModel{
		ID:     types.StringValue(ot.URN),
		URN:    types.StringValue(ot.URN),
		TypeID: types.StringValue(ot.ID),
	}
	applyOwnershipTypeToModel(ot, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// applyOwnershipTypeToModel maps a read OwnershipType onto the model,
// normalising the optional description field to null when empty to avoid
// spurious drift.
func applyOwnershipTypeToModel(ot *datahub.OwnershipType, m *ownershipTypeResourceModel) {
	m.URN = types.StringValue(ot.URN)
	m.ID = types.StringValue(ot.URN)
	m.TypeID = types.StringValue(ot.ID)
	m.Name = types.StringValue(ot.Name)
	m.Description = nullIfEmpty(ot.Description)
}
