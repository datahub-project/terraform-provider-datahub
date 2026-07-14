// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

const assertionURNPrefix = "urn:li:assertion:"

var (
	_ resource.Resource                = &customAssertionResource{}
	_ resource.ResourceWithConfigure   = &customAssertionResource{}
	_ resource.ResourceWithImportState = &customAssertionResource{}
)

type customAssertionResource struct {
	client *datahub.Client
}

type customAssertionResourceModel struct {
	ID               types.String `tfsdk:"id"`
	URN              types.String `tfsdk:"urn"`
	EntityURN        types.String `tfsdk:"entity_urn"`
	AssertionType    types.String `tfsdk:"assertion_type"`
	Description      types.String `tfsdk:"description"`
	FieldPath        types.String `tfsdk:"field_path"`
	PlatformURN      types.String `tfsdk:"platform_urn"`
	ExternalURL      types.String `tfsdk:"external_url"`
	Logic            types.String `tfsdk:"logic"`
	OnSuccessActions types.List   `tfsdk:"on_success_actions"`
	OnFailureActions types.List   `tfsdk:"on_failure_actions"`
}

// NewCustomAssertionResource returns a new datahub_custom_assertion resource.
func NewCustomAssertionResource() resource.Resource {
	return &customAssertionResource{}
}

func (r *customAssertionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	client := pd.Client
	r.client = client
}

func (r *customAssertionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_custom_assertion"
}

func (r *customAssertionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a custom (external) DataHub assertion.\n\n" +
			"Custom assertions are evaluated by an external system (e.g. dbt tests, " +
			"Great Expectations, a custom script) and reported back to DataHub via the " +
			"`reportAssertionResult` API. This resource declares the assertion definition " +
			"and associates it with a dataset; it does not run the assertion itself.\n\n" +
			"## URN\n\n" +
			"DataHub generates a server-side UUID for each assertion. The `urn` and `id` " +
			"attributes are populated after creation and are stable across updates. " +
			"ImportState requires the full assertion URN (e.g. `urn:li:assertion:<uuid>`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this assertion (e.g. `urn:li:assertion:<uuid>`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"entity_urn": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "URN of the DataHub dataset this assertion monitors.",
			},
			"assertion_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Type label for the assertion (e.g. `CUSTOM`, `FRESHNESS`, `VOLUME`). Arbitrary string used for display and filtering in DataHub.",
			},
			"description": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable description of what this assertion checks.",
			},
			"field_path": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Column or field path this assertion relates to. Omit for table-level assertions.",
			},
			"platform_urn": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "URN of the data platform that evaluates this assertion (e.g. `urn:li:dataPlatform:dbt`).",
			},
			"external_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "URL linking to the assertion definition in an external system.",
			},
			"logic": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Logic description or expression for the assertion.",
			},
			"on_success_actions": schema.ListAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Actions to take when the assertion passes (e.g. `[\"RESOLVE_INCIDENT\"]`).",
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"on_failure_actions": schema.ListAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Actions to take when the assertion fails (e.g. `[\"RAISE_INCIDENT\"]`).",
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *customAssertionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan customAssertionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn, err := r.client.UpsertCustomAssertion(ctx, datahub.UpsertCustomAssertionInput{
		EntityURN:     plan.EntityURN.ValueString(),
		AssertionType: plan.AssertionType.ValueString(),
		Description:   plan.Description.ValueString(),
		FieldPath:     strVal(plan.FieldPath),
		PlatformURN:   plan.PlatformURN.ValueString(),
		ExternalURL:   strVal(plan.ExternalURL),
		Logic:         strVal(plan.Logic),
	})
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = types.StringValue(urn)
	plan.URN = types.StringValue(urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *customAssertionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state customAssertionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}

	ai, err := r.client.GetAssertionByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if ai == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	d := applyCustomAssertionToModel(ctx, ai, &state)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *customAssertionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan, state customAssertionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.UpsertCustomAssertion(ctx, datahub.UpsertCustomAssertionInput{
		ExistingURN:   state.URN.ValueString(),
		EntityURN:     plan.EntityURN.ValueString(),
		AssertionType: plan.AssertionType.ValueString(),
		Description:   plan.Description.ValueString(),
		FieldPath:     strVal(plan.FieldPath),
		PlatformURN:   plan.PlatformURN.ValueString(),
		ExternalURL:   strVal(plan.ExternalURL),
		Logic:         strVal(plan.Logic),
	})
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = state.ID
	plan.URN = state.URN
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *customAssertionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state customAssertionResourceModel
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

	if err := r.client.DeleteAssertion(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *customAssertionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" || !strings.HasPrefix(raw, assertionURNPrefix) {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			"Expected a full DataHub assertion URN (e.g. urn:li:assertion:<uuid>). "+
				"Bare UUID import is not supported because assertion IDs are server-generated.",
		)
		return
	}

	ai, err := r.client.GetAssertionByURN(ctx, raw)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if ai == nil {
		resp.Diagnostics.AddError(
			"Assertion not found",
			fmt.Sprintf("No assertion with URN %q was found in DataHub. Verify the URN and retry.", raw),
		)
		return
	}

	if ai.Custom == nil {
		resp.Diagnostics.AddError(
			"Wrong assertion type",
			fmt.Sprintf("URN %q is a %q assertion, not a custom assertion. Use the appropriate resource type.", raw, ai.Type),
		)
		return
	}

	state := customAssertionResourceModel{
		ID:  types.StringValue(ai.URN),
		URN: types.StringValue(ai.URN),
	}
	d := applyCustomAssertionToModel(ctx, ai, &state)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func applyCustomAssertionToModel(ctx context.Context, ai *datahub.AssertionInfo, m *customAssertionResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	m.URN = types.StringValue(ai.URN)
	m.ID = types.StringValue(ai.URN)
	m.EntityURN = types.StringValue(ai.EntityURN)

	if ai.Custom != nil {
		m.AssertionType = types.StringValue(ai.Custom.AssertionType)
		m.Description = types.StringValue(ai.Custom.Description)
		// FieldPath is not read back: the API stores it as a full schema-field URN
		// (e.g. urn:li:schemaField:(...)) which cannot be safely round-tripped to
		// the simple field name the user supplied. Leave m.FieldPath unchanged so
		// the prior state value is preserved and no spurious drift is detected.
		m.PlatformURN = types.StringValue(ai.Custom.PlatformURN)
		m.ExternalURL = nullIfEmpty(ai.Custom.ExternalURL)
		m.Logic = nullIfEmpty(ai.Custom.Logic)
	}

	onSuccess, d := stringsToList(ctx, ai.OnSuccessActions)
	diags.Append(d...)
	m.OnSuccessActions = onSuccess

	onFailure, d := stringsToList(ctx, ai.OnFailureActions)
	diags.Append(d...)
	m.OnFailureActions = onFailure

	return diags
}

// listToStrings converts a types.List of strings to a []string.
func listToStrings(ctx context.Context, l types.List) ([]string, diag.Diagnostics) {
	if l.IsNull() || l.IsUnknown() {
		return nil, nil
	}
	out := make([]string, 0, len(l.Elements()))
	diags := l.ElementsAs(ctx, &out, false)
	return out, diags
}

// stringsToList converts a []string to a types.List of strings.
// An empty input yields a null list.
func stringsToList(_ context.Context, in []string) (types.List, diag.Diagnostics) {
	if len(in) == 0 {
		return types.ListNull(types.StringType), nil
	}
	elems := make([]attr.Value, len(in))
	for i, s := range in {
		elems[i] = types.StringValue(s)
	}
	return types.ListValue(types.StringType, elems)
}
