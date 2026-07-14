// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ resource.Resource                   = &fieldAssertionResource{}
	_ resource.ResourceWithConfigure      = &fieldAssertionResource{}
	_ resource.ResourceWithImportState    = &fieldAssertionResource{}
	_ resource.ResourceWithValidateConfig = &fieldAssertionResource{}
)

type fieldAssertionResource struct {
	client *datahub.Client
}

type fieldAssertionResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	URN                types.String `tfsdk:"urn"`
	EntityURN          types.String `tfsdk:"entity_urn"`
	Description        types.String `tfsdk:"description"`
	FieldAssertionType types.String `tfsdk:"field_assertion_type"`
	FieldPath          types.String `tfsdk:"field_path"`
	FieldType          types.String `tfsdk:"field_type"`
	FieldNativeType    types.String `tfsdk:"field_native_type"`
	Operator           types.String `tfsdk:"operator"`
	MinValue           types.String `tfsdk:"min_value"`
	MaxValue           types.String `tfsdk:"max_value"`
	SingleValue        types.String `tfsdk:"single_value"`
	Metric             types.String `tfsdk:"metric"`
	TransformType      types.String `tfsdk:"transform_type"`
	FailThresholdType  types.String `tfsdk:"fail_threshold_type"`
	FailThresholdValue types.Int64  `tfsdk:"fail_threshold_value"`
	ExcludeNulls       types.Bool   `tfsdk:"exclude_nulls"`
	SourceType         types.String `tfsdk:"source_type"`
	FilterSQL          types.String `tfsdk:"filter_sql"`
	FailureSeverity    types.String `tfsdk:"failure_severity"`
	EvaluationCron     types.String `tfsdk:"evaluation_cron"`
	EvaluationTimezone types.String `tfsdk:"evaluation_timezone"`
	OnSuccessActions   types.List   `tfsdk:"on_success_actions"`
	OnFailureActions   types.List   `tfsdk:"on_failure_actions"`
	Mode               types.String `tfsdk:"mode"`
	ExecutorID         types.String `tfsdk:"executor_id"`
}

// NewFieldAssertionResource returns a new datahub_field_assertion resource.
func NewFieldAssertionResource() resource.Resource {
	return &fieldAssertionResource{}
}

func (r *fieldAssertionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	client := pd.Client
	r.client = client
}

func (r *fieldAssertionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_field_assertion"
}

func (r *fieldAssertionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: cloudOnlyBadge +
			"Creates and manages a DataHub field (column) assertion monitor on a dataset.\n\n" +
			"Field assertions check a single column. `FIELD_VALUES` evaluates every row's " +
			"value against an operator (e.g. `id >= 0`); `FIELD_METRIC` evaluates an " +
			"aggregate column metric (e.g. `NULL_COUNT = 0`).\n\n" +
			"## Sub-types\n\n" +
			"Set `field_assertion_type = \"FIELD_VALUES\"` with `operator` + a value " +
			"(`single_value` or `min_value`/`max_value`), optionally `transform_type`, " +
			"`fail_threshold_type`/`fail_threshold_value`, and `exclude_nulls`. " +
			"Set `field_assertion_type = \"FIELD_METRIC\"` with `metric` + `operator` + a value. " +
			"FIELD_VALUES requires a warehouse-backed `source_type` and platform.\n\n" +
			"## URN\n\n" +
			"DataHub generates a server-side UUID for each assertion. The `urn` and `id` " +
			"attributes are populated after creation and are stable across updates. " +
			"ImportState requires the full assertion URN (e.g. `urn:li:assertion:<uuid>`). " +
			"Only NATIVE (author-as-code) assertions can be imported; ingested EXTERNAL " +
			"or smart/AI INFERRED assertions are refused.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this assertion (e.g. `urn:li:assertion:<uuid>`).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"entity_urn": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "URN of the DataHub dataset this assertion monitors.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Human-readable description of what this field assertion checks.",
			},
			"field_assertion_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Field assertion sub-type: `FIELD_VALUES` (per-row value check) or `FIELD_METRIC` (aggregate column metric).",
			},
			"field_path": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Path (name) of the column this assertion checks.",
			},
			"field_type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "DataHub standard type of the column: `STRING`, `NUMBER`, `BOOLEAN`, " +
					"`DATE`, `TIME`, `BYTES`, `ENUM`, `FIXED`, `MAP`, `ARRAY`, `UNION`, `STRUCT`, or `NULL`.",
			},
			"field_native_type": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Platform-native column type (e.g. `VARCHAR`, `INTEGER`).",
			},
			"operator": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Comparison operator, e.g. `EQUAL_TO`, `GREATER_THAN_OR_EQUAL_TO`, `BETWEEN`, `IN`, `NOT_NULL`.",
			},
			"min_value": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Minimum value (used with `operator = \"BETWEEN\"`).",
			},
			"max_value": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Maximum value (used with `operator = \"BETWEEN\"`).",
			},
			"single_value": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Comparison value (used with non-BETWEEN operators).",
			},
			"metric": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Column metric for `FIELD_METRIC`: one of `UNIQUE_COUNT`, `UNIQUE_PERCENTAGE`, " +
					"`NULL_COUNT`, `NULL_PERCENTAGE`, `MIN`, `MAX`, `MEAN`, `MEDIAN`, `STDDEV`, " +
					"`NEGATIVE_COUNT`, `NEGATIVE_PERCENTAGE`, `ZERO_COUNT`, `ZERO_PERCENTAGE`, " +
					"`MIN_LENGTH`, `MAX_LENGTH`, `EMPTY_COUNT`, `EMPTY_PERCENTAGE`. Required for FIELD_METRIC.",
			},
			"transform_type": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional FIELD_VALUES transform applied before comparison. Currently `LENGTH`.",
			},
			"fail_threshold_type": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "FIELD_VALUES failure threshold mode: `COUNT` or `PERCENTAGE` of failing rows tolerated.",
			},
			"fail_threshold_value": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "FIELD_VALUES failure threshold value (paired with `fail_threshold_type`).",
			},
			"exclude_nulls": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "FIELD_VALUES: whether null values are excluded from evaluation. Defaults to `false`.",
			},
			"source_type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "How DataHub reads the column: `ALL_ROWS_QUERY`, `CHANGED_ROWS_QUERY`, " +
					"`TABLE_STATISTICS`, or `DATAHUB_DATASET_PROFILE`. FIELD_VALUES requires a query source.",
			},
			"filter_sql": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional SQL `WHERE` clause (without the `WHERE` keyword) restricting which rows are checked.",
			},
			"failure_severity": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Severity raised when this assertion fails: `LOW`, `MEDIUM`, or `HIGH`. Omit for the DataHub default.",
			},
			"evaluation_cron": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Cron expression defining when DataHub evaluates this assertion.",
			},
			"evaluation_timezone": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Timezone for the evaluation cron schedule (e.g. `\"UTC\"`).",
			},
			"on_success_actions": schema.ListAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Actions to take when the assertion passes (e.g. `[\"RESOLVE_INCIDENT\"]`).",
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"on_failure_actions": schema.ListAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Actions to take when the assertion fails (e.g. `[\"RAISE_INCIDENT\"]`).",
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
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

// ValidateConfig enforces the FIELD_VALUES / FIELD_METRIC attribute split.
func (r *fieldAssertionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg fieldAssertionResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() || cfg.FieldAssertionType.IsUnknown() {
		return
	}

	hasMetric := !cfg.Metric.IsNull() && cfg.Metric.ValueString() != ""
	hasTransform := !cfg.TransformType.IsNull() && cfg.TransformType.ValueString() != ""
	hasThreshType := !cfg.FailThresholdType.IsNull() && cfg.FailThresholdType.ValueString() != ""
	hasThreshVal := !cfg.FailThresholdValue.IsNull()

	switch cfg.FieldAssertionType.ValueString() {
	case "FIELD_METRIC":
		if !hasMetric {
			resp.Diagnostics.AddAttributeError(path.Root("metric"), "Missing metric",
				"metric is required when field_assertion_type = \"FIELD_METRIC\".")
		}
		if hasTransform {
			resp.Diagnostics.AddAttributeError(path.Root("transform_type"), "Unexpected attribute",
				"transform_type is only valid for FIELD_VALUES.")
		}
		if hasThreshType || hasThreshVal {
			resp.Diagnostics.AddAttributeError(path.Root("fail_threshold_type"), "Unexpected attribute",
				"fail_threshold_type/fail_threshold_value are only valid for FIELD_VALUES.")
		}
	case "FIELD_VALUES":
		if hasMetric {
			resp.Diagnostics.AddAttributeError(path.Root("metric"), "Unexpected attribute",
				"metric is only valid for FIELD_METRIC.")
		}
		if hasThreshType != hasThreshVal {
			resp.Diagnostics.AddAttributeError(path.Root("fail_threshold_type"), "Incomplete fail threshold",
				"fail_threshold_type and fail_threshold_value must be set together.")
		}
	}
}

func (r *fieldAssertionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var plan fieldAssertionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, d := r.buildInput(ctx, plan, "")
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn, err := r.client.UpsertFieldAssertion(ctx, in)
	if err != nil {
		if errors.Is(err, datahub.ErrAssertionCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required",
				"datahub_field_assertion requires DataHub Cloud. "+
					"The configured DataHub instance does not support assertion monitor management.")
			return
		}
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = types.StringValue(urn)
	plan.URN = types.StringValue(urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *fieldAssertionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var state fieldAssertionResourceModel
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
	if d, bad := nonNativeAssertionError(urn, ai.Source); bad {
		resp.Diagnostics.Append(d)
		return
	}

	if ai.Field != nil {
		applyFieldInfo(&state, ai.Field)
	}
	state.Description = nullIfEmpty(ai.Description)
	state.FilterSQL = nullIfEmpty(ai.FilterSQL)
	state.EntityURN = types.StringValue(ai.EntityURN)
	state.URN = types.StringValue(ai.URN)
	state.ID = types.StringValue(ai.URN)

	onSuccess, d := stringsToList(ctx, ai.OnSuccessActions)
	resp.Diagnostics.Append(d...)
	state.OnSuccessActions = onSuccess
	onFailure, d := stringsToList(ctx, ai.OnFailureActions)
	resp.Diagnostics.Append(d...)
	state.OnFailureActions = onFailure

	mon, err := r.client.GetAssertionMonitor(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if mon != nil {
		state.EvaluationCron = nullIfEmpty(mon.EvaluationCron)
		state.EvaluationTimezone = nullIfEmpty(mon.EvaluationTimezone)
		if mon.SourceType != "" {
			state.SourceType = types.StringValue(mon.SourceType)
		}
		if mon.Mode != "" {
			state.Mode = types.StringValue(mon.Mode)
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *fieldAssertionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var plan, state fieldAssertionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, d := r.buildInput(ctx, plan, state.URN.ValueString())
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.UpsertFieldAssertion(ctx, in)
	if err != nil {
		if errors.Is(err, datahub.ErrAssertionCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required", "datahub_field_assertion requires DataHub Cloud.")
			return
		}
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = state.ID
	plan.URN = state.URN
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *fieldAssertionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var state fieldAssertionResourceModel
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

func (r *fieldAssertionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
		resp.Diagnostics.AddError("Assertion not found", fmt.Sprintf("No assertion with URN %q was found in DataHub.", raw))
		return
	}
	if d, bad := nonNativeAssertionError(raw, ai.Source); bad {
		resp.Diagnostics.Append(d)
		return
	}
	if ai.Field == nil {
		resp.Diagnostics.AddError("Wrong assertion type",
			fmt.Sprintf("URN %q is a %q assertion, not a field assertion.", raw, ai.Type))
		return
	}

	onSuccess, d := stringsToList(ctx, ai.OnSuccessActions)
	resp.Diagnostics.Append(d...)
	onFailure, d := stringsToList(ctx, ai.OnFailureActions)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := fieldAssertionResourceModel{
		ID:                 types.StringValue(ai.URN),
		URN:                types.StringValue(ai.URN),
		EntityURN:          types.StringValue(ai.EntityURN),
		Description:        nullIfEmpty(ai.Description),
		FilterSQL:          nullIfEmpty(ai.FilterSQL),
		OnSuccessActions:   onSuccess,
		OnFailureActions:   onFailure,
		EvaluationCron:     types.StringValue(""),
		EvaluationTimezone: types.StringValue("UTC"),
		SourceType:         types.StringValue("ALL_ROWS_QUERY"),
		Mode:               types.StringValue("ACTIVE"),
		ExcludeNulls:       types.BoolValue(false),
	}
	applyFieldInfo(&state, ai.Field)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// applyFieldInfo copies a read-shape FieldAssertionInfo onto a resource model.
func applyFieldInfo(state *fieldAssertionResourceModel, f *datahub.FieldAssertionInfo) {
	state.FieldAssertionType = types.StringValue(f.FieldType)
	state.FieldPath = types.StringValue(f.FieldPath)
	state.FieldType = nullIfEmpty(f.StdType)
	state.FieldNativeType = nullIfEmpty(f.NativeType)
	state.Operator = nullIfEmpty(f.Operator)
	state.MinValue = nullIfEmpty(f.MinValue)
	state.MaxValue = nullIfEmpty(f.MaxValue)
	state.SingleValue = nullIfEmpty(f.Value)
	state.Metric = nullIfEmpty(f.Metric)
	state.FailureSeverity = nullIfEmpty(f.FailureSeverity)
	state.TransformType = nullIfEmpty(f.TransformType)
	state.FailThresholdType = nullIfEmpty(f.FailThreshold)
	if f.FailThreshold != "" {
		state.FailThresholdValue = types.Int64Value(f.FailThresholdN)
	} else {
		state.FailThresholdValue = types.Int64Null()
	}
	state.ExcludeNulls = types.BoolValue(f.ExcludeNulls)
}

// buildInput assembles the client input from a plan model.
func (r *fieldAssertionResource) buildInput(ctx context.Context, plan fieldAssertionResourceModel, assertionURN string) (datahub.FieldAssertionInput, diag.Diagnostics) {
	var diags diag.Diagnostics
	onSuccess, d := listToStrings(ctx, plan.OnSuccessActions)
	diags.Append(d...)
	onFailure, d := listToStrings(ctx, plan.OnFailureActions)
	diags.Append(d...)

	in := datahub.FieldAssertionInput{
		AssertionURN:       assertionURN,
		EntityURN:          plan.EntityURN.ValueString(),
		Description:        strVal(plan.Description),
		FieldType:          plan.FieldAssertionType.ValueString(),
		FieldPath:          plan.FieldPath.ValueString(),
		StdType:            plan.FieldType.ValueString(),
		NativeType:         strVal(plan.FieldNativeType),
		Operator:           plan.Operator.ValueString(),
		MinValue:           strVal(plan.MinValue),
		MaxValue:           strVal(plan.MaxValue),
		SingleValue:        strVal(plan.SingleValue),
		Metric:             strVal(plan.Metric),
		TransformType:      strVal(plan.TransformType),
		FailThreshold:      strVal(plan.FailThresholdType),
		ExcludeNulls:       plan.ExcludeNulls.ValueBool(),
		SourceType:         plan.SourceType.ValueString(),
		FilterSQL:          strVal(plan.FilterSQL),
		FailureSeverity:    strVal(plan.FailureSeverity),
		EvaluationCron:     plan.EvaluationCron.ValueString(),
		EvaluationTimezone: plan.EvaluationTimezone.ValueString(),
		OnSuccessActions:   onSuccess,
		OnFailureActions:   onFailure,
		Mode:               plan.Mode.ValueString(),
		ExecutorID:         strVal(plan.ExecutorID),
	}
	if !plan.FailThresholdValue.IsNull() {
		in.FailThresholdN = plan.FailThresholdValue.ValueInt64()
	}
	return in, diags
}
