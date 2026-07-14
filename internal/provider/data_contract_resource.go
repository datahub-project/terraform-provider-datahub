// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ resource.Resource                = &dataContractResource{}
	_ resource.ResourceWithConfigure   = &dataContractResource{}
	_ resource.ResourceWithImportState = &dataContractResource{}
)

type dataContractResource struct {
	client *datahub.Client
}

type dataContractResourceModel struct {
	ID                       types.String `tfsdk:"id"`
	URN                      types.String `tfsdk:"urn"`
	DatasetURN               types.String `tfsdk:"dataset_urn"`
	State                    types.String `tfsdk:"state"`
	FreshnessAssertionURNs   types.List   `tfsdk:"freshness_assertion_urns"`
	SchemaAssertionURNs      types.List   `tfsdk:"schema_assertion_urns"`
	DataQualityAssertionURNs types.List   `tfsdk:"data_quality_assertion_urns"`
}

// NewDataContractResource returns a new datahub_data_contract resource.
func NewDataContractResource() resource.Resource {
	return &dataContractResource{}
}

func (r *dataContractResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	client := pd.Client
	r.client = client
}

func (r *dataContractResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_contract"
}

func assertionURNListAttribute(category string) schema.ListAttribute {
	return schema.ListAttribute{
		ElementType: types.StringType,
		Optional:    true,
		MarkdownDescription: fmt.Sprintf(
			"URNs of existing assertions to bind under the contract's **%s** category "+
				"(e.g. `[datahub_freshness_assertion.x.urn]`). Reference assertion resources by "+
				"`.urn` so Terraform creates them before the contract. The resource owns the "+
				"complete list -- assertions removed here are unbound from the contract on the next apply.", category),
	}
}

func (r *dataContractResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a DataHub **data contract** -- a per-dataset bundle that groups " +
			"existing assertions into freshness, schema, and data-quality guarantees plus a lifecycle " +
			"`state`. It is the declarative \"this dataset's SLA is X\" object a platform team pins in code.\n\n" +
			"A data contract does **not** create assertions; it references assertions authored elsewhere " +
			"(the typed `datahub_*_assertion` resources). The three category lists map to DataHub's fixed " +
			"contract buckets: freshness assertions to `freshness`, schema assertions to `schema`, and " +
			"volume/field/SQL/custom assertions to `data_quality`.\n\n" +
			"The contract entity is available on both OSS DataHub and DataHub Cloud. Note that most typed " +
			"assertion resources are Cloud-only; `datahub_custom_assertion` is the OSS-compatible one.\n\n" +
			"## URN\n\n" +
			"One contract per dataset. The URN is `urn:li:dataContract:<id>` where `id` defaults to a " +
			"deterministic hash of `dataset_urn` (matching the DataHub Python SDK), so a Terraform-managed " +
			"contract and an SDK-created one for the same dataset are the same entity. ImportState accepts " +
			"the full contract URN.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Contract id (URN suffix). Defaults to a deterministic hash of `dataset_urn`. " +
					"Set explicitly only to adopt an existing non-standard id. Changing it forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN (`urn:li:dataContract:<id>`).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"dataset_urn": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "URN of the dataset this contract applies to " +
					"(e.g. `datahub_dataset` URN or a raw `urn:li:dataset:(...)`). Changing it forces a new resource.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"state": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("ACTIVE"),
				MarkdownDescription: "Contract lifecycle state: `ACTIVE` (in force) or `PENDING` (proposed). Defaults to `ACTIVE`.",
				Validators:          []validator.String{enumString("ACTIVE", "PENDING")},
			},
			"freshness_assertion_urns":    assertionURNListAttribute("freshness"),
			"schema_assertion_urns":       assertionURNListAttribute("schema"),
			"data_quality_assertion_urns": assertionURNListAttribute("data quality"),
		},
	}
}

// buildInput converts the plan model to a client input, resolving the contract id.
func (r *dataContractResource) buildInput(ctx context.Context, plan dataContractResourceModel) (datahub.DataContractInput, string, diag.Diagnostics) {
	var diags diag.Diagnostics
	datasetURN := strings.TrimSpace(plan.DatasetURN.ValueString())
	if datasetURN == "" {
		diags.AddError("Invalid plan", "dataset_urn is required")
		return datahub.DataContractInput{}, "", diags
	}

	id := strings.TrimSpace(plan.ID.ValueString())
	if id == "" {
		derived, err := datahub.DataContractID(datasetURN)
		if err != nil {
			diags.AddError("Invalid plan", err.Error())
			return datahub.DataContractInput{}, "", diags
		}
		id = derived
	}

	freshness, dg := listToStrings(ctx, plan.FreshnessAssertionURNs)
	diags.Append(dg...)
	schemaURNs, dg2 := listToStrings(ctx, plan.SchemaAssertionURNs)
	diags.Append(dg2...)
	dq, dg3 := listToStrings(ctx, plan.DataQualityAssertionURNs)
	diags.Append(dg3...)

	in := datahub.DataContractInput{
		ID:                       id,
		EntityURN:                datasetURN,
		State:                    plan.State.ValueString(),
		FreshnessAssertionURNs:   freshness,
		SchemaAssertionURNs:      schemaURNs,
		DataQualityAssertionURNs: dq,
	}
	return in, id, diags
}

func (r *dataContractResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var plan dataContractResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.upsert(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dataContractResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var plan dataContractResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.upsert(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dataContractResource) upsert(ctx context.Context, plan *dataContractResourceModel, diags *diag.Diagnostics) {
	in, id, d := r.buildInput(ctx, *plan)
	diags.Append(d...)
	if diags.HasError() {
		return
	}

	urn, err := r.client.UpsertDataContract(ctx, in)
	if err != nil {
		diags.AddError("DataHub API Error", err.Error())
		return
	}
	plan.ID = types.StringValue(id)
	plan.URN = types.StringValue(urn)
}

func (r *dataContractResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var state dataContractResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := strings.TrimSpace(state.URN.ValueString())
	if urn == "" {
		urn = datahub.DataContractURNPrefix + strings.TrimSpace(state.ID.ValueString())
	}

	dc, err := r.client.GetDataContractByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if dc == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	r.applyToModel(ctx, dc, &state, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *dataContractResource) applyToModel(ctx context.Context, dc *datahub.DataContract, model *dataContractResourceModel, diags *diag.Diagnostics) {
	model.ID = types.StringValue(dc.ID)
	model.URN = types.StringValue(dc.URN)
	if dc.EntityURN != "" {
		model.DatasetURN = types.StringValue(dc.EntityURN)
	}
	if dc.State != "" {
		model.State = types.StringValue(dc.State)
	}
	freshness, d := stringsToList(ctx, dc.FreshnessAssertionURNs)
	diags.Append(d...)
	model.FreshnessAssertionURNs = freshness
	schemaURNs, d := stringsToList(ctx, dc.SchemaAssertionURNs)
	diags.Append(d...)
	model.SchemaAssertionURNs = schemaURNs
	dq, d := stringsToList(ctx, dc.DataQualityAssertionURNs)
	diags.Append(d...)
	model.DataQualityAssertionURNs = dq
}

func (r *dataContractResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var state dataContractResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	urn := strings.TrimSpace(state.URN.ValueString())
	if urn == "" {
		resp.State.RemoveResource(ctx)
		return
	}
	if err := r.client.DeleteDataContract(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *dataContractResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID",
			"Expected a DataHub data contract URN (e.g. urn:li:dataContract:<id>) or a bare contract id.")
		return
	}
	// Accept either the full URN or a bare id (the extract tool emits the bare id).
	id := strings.TrimPrefix(raw, datahub.DataContractURNPrefix)
	if id == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Could not extract a contract id from the provided import ID.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("urn"), datahub.DataContractURNPrefix+id)...)
}
