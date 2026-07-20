// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

const dataProductURNPrefix = "urn:li:dataProduct:"

var (
	_ resource.Resource                = &dataProductResource{}
	_ resource.ResourceWithConfigure   = &dataProductResource{}
	_ resource.ResourceWithImportState = &dataProductResource{}
	_ resource.ResourceWithModifyPlan  = &dataProductResource{}
)

const dataProductEntityPath = "dataproduct"

type dataProductResource struct {
	pd       *providerData
	client   *datahub.Client
	defaults entityDefaults
}

type dataProductResourceModel struct {
	ID                  types.String `tfsdk:"id"`
	URN                 types.String `tfsdk:"urn"`
	DataProductID       types.String `tfsdk:"data_product_id"`
	Name                types.String `tfsdk:"name"`
	Description         types.String `tfsdk:"description"`
	ExternalURL         types.String `tfsdk:"external_url"`
	CustomProperties    types.Map    `tfsdk:"custom_properties"`
	CustomPropertiesAll types.Map    `tfsdk:"custom_properties_all"`
	TagsAll             types.Set    `tfsdk:"tags_all"`
	Domain              types.String `tfsdk:"domain"`
}

func NewDataProductResource() resource.Resource {
	return &dataProductResource{}
}

func (r *dataProductResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	r.pd = pd
	r.client = pd.Client
	r.defaults = pd.defaults
}

func (r *dataProductResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy plan
	}
	planCustomPropertiesAll(ctx, r.defaults, req, resp)
	planTagsAll(ctx, r.defaults, resp)
}

func (r *dataProductResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_product"
}

func (r *dataProductResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a DataHub data product.\n\n" +
			"A data product is a curated, domain-scoped grouping of data assets with a " +
			"stable identity, description, and external documentation link. Data products " +
			"are the primary way to publish well-defined, discoverable data offerings in " +
			"DataHub's data mesh model.\n\n" +
			"## Naming\n\n" +
			"`data_product_id` becomes the URN suffix (`urn:li:dataProduct:<data_product_id>`). " +
			"Supply a stable human-readable slug (e.g. `orders-v2`) rather than a UUID. This " +
			"matches the DataHub Python SDK convention (`make_data_product_urn`) and ensures " +
			"the URN is predictable and importable across environments.\n\n" +
			"The DataHub UI creates data products with a random UUID when no id is supplied. " +
			"To import a UI-created data product, use the UUID from its URN as `data_product_id`.\n\n" +
			"## Asset membership\n\n" +
			"This resource manages the data product definition only: id, name, description, " +
			"domain, external URL, and custom properties. It does **not** manage the list of " +
			"member assets (datasets, charts, etc.) or their output-port flags. Asset " +
			"membership is intended to be set via the DataHub UI, CLI, or SDK. Membership " +
			"managed outside Terraform will not be affected by `terraform apply`.\n\n" +
			"## Write path\n\n" +
			"Create and update write the `dataProductProperties` aspect (and optionally the " +
			"`domains` aspect) directly via the DataHub OpenAPI v3 endpoint with the " +
			"user-supplied `data_product_id`. The GraphQL `createDataProduct` and " +
			"`updateDataProduct` mutations are not used because they cannot set `external_url` " +
			"or `custom_properties`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this data product (e.g. `urn:li:dataProduct:orders-v2`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"data_product_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Unique identifier for the data product. Becomes the URN suffix " +
					"(`urn:li:dataProduct:<data_product_id>`). Use a stable human-readable slug " +
					"(e.g. `orders-v2`). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable display name for the data product, shown throughout the DataHub UI.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the data product's purpose, contents, and intended consumers.",
			},
			"external_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "URL of external documentation or a data product catalog page for this data product.",
			},
			"custom_properties": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Key-value map of custom metadata properties to attach to this data product. " +
					"Keys and values must be non-empty strings, and values must not be null. Omit the " +
					"attribute entirely (do not set an empty map) to attach no custom properties. " +
					"Provider-level defaults (`auto_properties` markers and `defaults.custom_properties`) " +
					"are merged in automatically; the effective written map is the computed " +
					"`custom_properties_all`.",
				Validators: []validator.Map{
					nonEmptyStringMapValidator{},
				},
			},
			"custom_properties_all": customPropertiesAllSchema(),
			"tags_all":              tagsAllSchema(),
			"domain": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Full DataHub URN of the domain that owns this data product (e.g. `urn:li:domain:finance`). Accepts a reference such as `datahub_domain.finance.urn` so Terraform can order creation automatically.",
			},
		},
	}
}

func (r *dataProductResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan dataProductResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	dataProductID := plan.DataProductID.ValueString()
	urn := dataProductURNPrefix + dataProductID

	all, customProps, diags := resolvePlannedCustomPropertiesAll(ctx, r.defaults, plan.CustomPropertiesAll, plan.CustomProperties, types.MapNull(types.StringType), true)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.CustomPropertiesAll = all

	if err := r.client.WriteDataProductProperties(
		ctx, urn,
		plan.Name.ValueString(),
		strVal(plan.Description),
		strVal(plan.ExternalURL),
		customProps,
		strVal(plan.Domain),
	); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	tagsAll, tagURNs, td := resolvePlannedTagsAll(ctx, r.defaults, plan.TagsAll)
	resp.Diagnostics.Append(td...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.TagsAll = tagsAll
	if len(tagURNs) > 0 {
		if err := r.pd.ensureTagsExist(ctx, tagURNs); err != nil {
			resp.Diagnostics.AddError("Invalid provider defaults.tags", err.Error())
			return
		}
		if err := r.client.SetGlobalTags(ctx, dataProductEntityPath, urn, tagURNs); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	plan.ID = types.StringValue(urn)
	plan.URN = types.StringValue(urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dataProductResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state dataProductResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}

	dp, err := r.client.GetDataProductByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if dp == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	applyDataProductToModel(dp, &state)
	tagsAll, err := readTagsAll(ctx, r.client, dataProductEntityPath, urn, state.TagsAll)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	state.TagsAll = tagsAll
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *dataProductResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan, state dataProductResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()

	all, customProps, diags := resolvePlannedCustomPropertiesAll(ctx, r.defaults, plan.CustomPropertiesAll, plan.CustomProperties, state.CustomPropertiesAll, false)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.CustomPropertiesAll = all

	if err := r.client.WriteDataProductProperties(
		ctx, urn,
		plan.Name.ValueString(),
		strVal(plan.Description),
		strVal(plan.ExternalURL),
		customProps,
		strVal(plan.Domain),
	); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	// Reconcile tags when the effective list changed. A null plan with a
	// non-null prior state clears the aspect and releases the ownership latch.
	if !plan.TagsAll.Equal(state.TagsAll) {
		tagsAll, tagURNs, td := resolvePlannedTagsAll(ctx, r.defaults, plan.TagsAll)
		resp.Diagnostics.Append(td...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.TagsAll = tagsAll
		if len(tagURNs) > 0 {
			if err := r.pd.ensureTagsExist(ctx, tagURNs); err != nil {
				resp.Diagnostics.AddError("Invalid provider defaults.tags", err.Error())
				return
			}
		}
		if err := r.client.SetGlobalTags(ctx, dataProductEntityPath, urn, tagURNs); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	plan.ID = state.ID
	plan.URN = state.URN
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dataProductResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state dataProductResourceModel
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

	if err := r.client.DeleteDataProduct(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *dataProductResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub data product URN (e.g. urn:li:dataProduct:orders-v2) or a bare data product ID.")
		return
	}

	var dataProductID, urn string
	if strings.HasPrefix(raw, dataProductURNPrefix) {
		urn = raw
		dataProductID = strings.TrimPrefix(raw, dataProductURNPrefix)
	} else {
		dataProductID = raw
		urn = dataProductURNPrefix + dataProductID
	}
	if dataProductID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub data product URN (e.g. urn:li:dataProduct:orders-v2) or a bare data product ID.")
		return
	}

	dp, err := r.client.GetDataProductByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if dp == nil {
		resp.Diagnostics.AddError(
			"Data product not found",
			fmt.Sprintf("No data product with URN %q was found in DataHub. Verify the ID or URN and retry.", urn),
		)
		return
	}

	state := dataProductResourceModel{
		ID:            types.StringValue(dp.URN),
		URN:           types.StringValue(dp.URN),
		DataProductID: types.StringValue(dp.ID),
	}
	applyDataProductToModel(dp, &state)
	// Import attribution: keys matching provider defaults or auto-property
	// markers are omitted from custom_properties so the first plan after
	// import is minimal.
	state.CustomProperties, state.CustomPropertiesAll = importCustomProperties(dp.CustomProperties, r.defaults)
	tagsAll, err := importTagsAll(ctx, r.client, r.defaults, dataProductEntityPath, dp.URN)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	state.TagsAll = tagsAll
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// applyDataProductToModel maps a read DataProduct onto the resource model,
// normalising optional fields to null when empty to avoid spurious drift.
// Custom properties are reconciled against the model's prior state: the
// user-facing attribute keeps only its own keys, the computed _all records
// the full server map.
func applyDataProductToModel(dp *datahub.DataProduct, m *dataProductResourceModel) {
	m.URN = types.StringValue(dp.URN)
	m.ID = types.StringValue(dp.URN)
	m.DataProductID = types.StringValue(dp.ID)
	m.Name = types.StringValue(dp.Name)
	m.Description = nullIfEmpty(dp.Description)
	m.ExternalURL = nullIfEmpty(dp.ExternalURL)
	m.Domain = nullIfEmpty(dp.Domain)
	m.CustomProperties, m.CustomPropertiesAll = reconcileCustomPropertiesRead(dp.CustomProperties, m.CustomProperties)
}

// mapValToStringMap converts a types.Map (string elements) to a plain
// map[string]string. Returns nil (no error) for a null or unknown map.
func mapValToStringMap(_ context.Context, mv types.Map) (map[string]string, diag.Diagnostics) {
	var diags diag.Diagnostics
	if mv.IsNull() || mv.IsUnknown() {
		return nil, diags
	}
	elems := mv.Elements()
	result := make(map[string]string, len(elems))
	for k, v := range elems {
		sv, ok := v.(types.String)
		if !ok {
			diags.AddError("Invalid custom_properties value", fmt.Sprintf("custom_properties[%q] is not a string", k))
			continue
		}
		result[k] = sv.ValueString()
	}
	return result, diags
}
