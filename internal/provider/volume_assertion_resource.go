// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ resource.Resource                = &volumeAssertionResource{}
	_ resource.ResourceWithConfigure   = &volumeAssertionResource{}
	_ resource.ResourceWithImportState = &volumeAssertionResource{}
)

type volumeAssertionResource struct {
	client *datahub.Client
}

type volumeAssertionResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	URN                types.String `tfsdk:"urn"`
	EntityURN          types.String `tfsdk:"entity_urn"`
	VolumeType         types.String `tfsdk:"volume_type"`
	Operator           types.String `tfsdk:"operator"`
	MinValue           types.String `tfsdk:"min_value"`
	MaxValue           types.String `tfsdk:"max_value"`
	SingleValue        types.String `tfsdk:"single_value"`
	EvaluationCron     types.String `tfsdk:"evaluation_cron"`
	EvaluationTimezone types.String `tfsdk:"evaluation_timezone"`
	SourceType         types.String `tfsdk:"source_type"`
	OnSuccessActions   types.List   `tfsdk:"on_success_actions"`
	OnFailureActions   types.List   `tfsdk:"on_failure_actions"`
	Mode               types.String `tfsdk:"mode"`
	ExecutorID         types.String `tfsdk:"executor_id"`
}

// NewVolumeAssertionResource returns a new datahub_volume_assertion resource.
func NewVolumeAssertionResource() resource.Resource {
	return &volumeAssertionResource{}
}

func (r *volumeAssertionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *volumeAssertionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_volume_assertion"
}

func (r *volumeAssertionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: cloudOnlyBadge +
			"Creates and manages a DataHub volume assertion monitor on a dataset.\n\n" +
			"Volume assertions check that a dataset has an expected number of rows at " +
			"scheduled evaluation times. DataHub evaluates the assertion against a " +
			"previously ingested `DatasetProfile` (set `source_type = \"DATAHUB_DATASET_PROFILE\"`) " +
			"or by querying the source system directly.\n\n" +
			"## URN\n\n" +
			"DataHub generates a server-side UUID for each assertion. The `urn` and `id` " +
			"attributes are populated after creation and are stable across updates. " +
			"ImportState requires the full assertion URN (e.g. `urn:li:assertion:<uuid>`).\n\n" +
			"## Operator and value attributes\n\n" +
			"For `BETWEEN` operator: supply `min_value` and `max_value`. " +
			"For all other operators (`GREATER_THAN`, `LESS_THAN`, `EQUAL_TO`, etc.): supply `single_value`.",
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
			"volume_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Volume assertion sub-type. Currently `ROW_COUNT_TOTAL` is supported.",
			},
			"operator": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Comparison operator. One of `BETWEEN`, `GREATER_THAN`, `GREATER_THAN_OR_EQUAL_TO`, `LESS_THAN`, `LESS_THAN_OR_EQUAL_TO`, `EQUAL_TO`.",
			},
			"min_value": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Minimum row count (used with `operator = \"BETWEEN\"`).",
			},
			"max_value": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Maximum row count (used with `operator = \"BETWEEN\"`).",
			},
			"single_value": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Row count threshold (used with non-BETWEEN operators).",
			},
			"evaluation_cron": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Cron expression defining when DataHub evaluates this assertion (e.g. `\"0 */8 * * *\"`).",
			},
			"evaluation_timezone": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Timezone for the evaluation cron schedule (e.g. `\"UTC\"`).",
			},
			"source_type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "How DataHub obtains the row count. " +
					"`DATAHUB_DATASET_PROFILE` uses a previously ingested DatasetProfile aspect (no live DB query needed). " +
					"`INFORMATION_SCHEMA` or `QUERY` queries the source system directly.",
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
			"mode": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Monitoring mode. `ACTIVE` enables scheduled evaluation. `PASSIVE` records results without scheduling.",
			},
			"executor_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "ID of the remote executor pool to use for evaluation. Omit to use the default executor.",
			},
		},
	}
}

func (r *volumeAssertionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan volumeAssertionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	onSuccess, d := listToStrings(ctx, plan.OnSuccessActions)
	resp.Diagnostics.Append(d...)
	onFailure, d := listToStrings(ctx, plan.OnFailureActions)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn, err := r.client.UpsertVolumeAssertion(ctx, datahub.VolumeAssertionInput{
		EntityURN:          plan.EntityURN.ValueString(),
		VolumeType:         plan.VolumeType.ValueString(),
		Operator:           plan.Operator.ValueString(),
		MinValue:           strVal(plan.MinValue),
		MaxValue:           strVal(plan.MaxValue),
		SingleValue:        strVal(plan.SingleValue),
		EvaluationCron:     plan.EvaluationCron.ValueString(),
		EvaluationTimezone: plan.EvaluationTimezone.ValueString(),
		SourceType:         plan.SourceType.ValueString(),
		OnSuccessActions:   onSuccess,
		OnFailureActions:   onFailure,
		Mode:               plan.Mode.ValueString(),
		ExecutorID:         strVal(plan.ExecutorID),
	})
	if err != nil {
		if errors.Is(err, datahub.ErrAssertionCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required",
				"datahub_volume_assertion requires DataHub Cloud. "+
					"The configured DataHub instance does not support assertion monitor management. "+
					"Use datahub_custom_assertion for OSS-compatible assertion tracking.")
			return
		}
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = types.StringValue(urn)
	plan.URN = types.StringValue(urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *volumeAssertionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state volumeAssertionResourceModel
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

	// Volume parameters come from assertionInfo; the monitor-side fields
	// (evaluation schedule, source type, mode) are recovered from the Monitor
	// entity below.
	if ai.Volume != nil {
		state.VolumeType = types.StringValue(ai.Volume.VolumeType)
		state.Operator = types.StringValue(ai.Volume.Operator)
		state.MinValue = nullIfEmpty(ai.Volume.MinValue)
		state.MaxValue = nullIfEmpty(ai.Volume.MaxValue)
		state.SingleValue = nullIfEmpty(ai.Volume.Value)
	}
	state.EntityURN = types.StringValue(ai.EntityURN)
	state.URN = types.StringValue(ai.URN)
	state.ID = types.StringValue(ai.URN)

	onSuccess, d := stringsToList(ctx, ai.OnSuccessActions)
	resp.Diagnostics.Append(d...)
	state.OnSuccessActions = onSuccess
	onFailure, d := stringsToList(ctx, ai.OnFailureActions)
	resp.Diagnostics.Append(d...)
	state.OnFailureActions = onFailure

	// Recover monitor-side fields (evaluation schedule, source type, mode) from
	// the associated Monitor entity so ImportState produces a clean plan.
	mon, err := r.client.GetAssertionMonitor(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if mon != nil {
		state.EvaluationCron = nullIfEmpty(mon.EvaluationCron)
		state.EvaluationTimezone = nullIfEmpty(mon.EvaluationTimezone)
		state.SourceType = nullIfEmpty(mon.SourceType)
		if mon.Mode != "" {
			state.Mode = types.StringValue(mon.Mode)
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *volumeAssertionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan, state volumeAssertionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	onSuccess, d := listToStrings(ctx, plan.OnSuccessActions)
	resp.Diagnostics.Append(d...)
	onFailure, d := listToStrings(ctx, plan.OnFailureActions)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.UpsertVolumeAssertion(ctx, datahub.VolumeAssertionInput{
		AssertionURN:       state.URN.ValueString(),
		EntityURN:          plan.EntityURN.ValueString(),
		VolumeType:         plan.VolumeType.ValueString(),
		Operator:           plan.Operator.ValueString(),
		MinValue:           strVal(plan.MinValue),
		MaxValue:           strVal(plan.MaxValue),
		SingleValue:        strVal(plan.SingleValue),
		EvaluationCron:     plan.EvaluationCron.ValueString(),
		EvaluationTimezone: plan.EvaluationTimezone.ValueString(),
		SourceType:         plan.SourceType.ValueString(),
		OnSuccessActions:   onSuccess,
		OnFailureActions:   onFailure,
		Mode:               plan.Mode.ValueString(),
		ExecutorID:         strVal(plan.ExecutorID),
	})
	if err != nil {
		if errors.Is(err, datahub.ErrAssertionCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required",
				"datahub_volume_assertion requires DataHub Cloud.")
			return
		}
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = state.ID
	plan.URN = state.URN
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *volumeAssertionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state volumeAssertionResourceModel
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

	if err := r.client.DeleteCloudAssertionWithMonitor(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *volumeAssertionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" || !strings.HasPrefix(raw, assertionURNPrefix) {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			"Expected a full DataHub assertion URN (e.g. urn:li:assertion:<uuid>).",
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
			fmt.Sprintf("No assertion with URN %q was found in DataHub.", raw),
		)
		return
	}

	if ai.Volume == nil {
		resp.Diagnostics.AddError(
			"Wrong assertion type",
			fmt.Sprintf("URN %q is a %q assertion, not a volume assertion.", raw, ai.Type),
		)
		return
	}

	onSuccess, d := stringsToList(ctx, ai.OnSuccessActions)
	resp.Diagnostics.Append(d...)
	onFailure, d := stringsToList(ctx, ai.OnFailureActions)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := volumeAssertionResourceModel{
		ID:               types.StringValue(ai.URN),
		URN:              types.StringValue(ai.URN),
		EntityURN:        types.StringValue(ai.EntityURN),
		OnSuccessActions: onSuccess,
		OnFailureActions: onFailure,
		// Monitor-only fields cannot be read from assertionInfo; use empty defaults.
		EvaluationCron:     types.StringValue(""),
		EvaluationTimezone: types.StringValue("UTC"),
		SourceType:         types.StringValue("DATAHUB_DATASET_PROFILE"),
		Mode:               types.StringValue("ACTIVE"),
	}
	if ai.Volume != nil {
		state.VolumeType = types.StringValue(ai.Volume.VolumeType)
		state.Operator = types.StringValue(ai.Volume.Operator)
		state.MinValue = nullIfEmpty(ai.Volume.MinValue)
		state.MaxValue = nullIfEmpty(ai.Volume.MaxValue)
		state.SingleValue = nullIfEmpty(ai.Volume.Value)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
