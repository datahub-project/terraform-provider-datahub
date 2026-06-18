// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ resource.Resource                   = &sqlAssertionResource{}
	_ resource.ResourceWithConfigure      = &sqlAssertionResource{}
	_ resource.ResourceWithImportState    = &sqlAssertionResource{}
	_ resource.ResourceWithValidateConfig = &sqlAssertionResource{}
)

type sqlAssertionResource struct {
	client *datahub.Client
}

type sqlAssertionResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	URN                types.String `tfsdk:"urn"`
	EntityURN          types.String `tfsdk:"entity_urn"`
	SQLType            types.String `tfsdk:"sql_type"`
	ChangeType         types.String `tfsdk:"change_type"`
	Statement          types.String `tfsdk:"statement"`
	Operator           types.String `tfsdk:"operator"`
	Value              types.String `tfsdk:"value"`
	Description        types.String `tfsdk:"description"`
	FailureSeverity    types.String `tfsdk:"failure_severity"`
	EvaluationCron     types.String `tfsdk:"evaluation_cron"`
	EvaluationTimezone types.String `tfsdk:"evaluation_timezone"`
	OnSuccessActions   types.List   `tfsdk:"on_success_actions"`
	OnFailureActions   types.List   `tfsdk:"on_failure_actions"`
	Mode               types.String `tfsdk:"mode"`
	ExecutorID         types.String `tfsdk:"executor_id"`
}

// NewSQLAssertionResource returns a new datahub_sql_assertion resource.
func NewSQLAssertionResource() resource.Resource {
	return &sqlAssertionResource{}
}

func (r *sqlAssertionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *sqlAssertionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sql_assertion"
}

func (r *sqlAssertionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: cloudOnlyBadge +
			"Creates and manages a DataHub SQL assertion monitor on a dataset.\n\n" +
			"SQL assertions run a custom SQL query against the dataset and compare the " +
			"numeric result to an expected value. Use them for business-logic checks that " +
			"volume and freshness assertions cannot express (e.g. no negative values, " +
			"referential integrity counts).\n\n" +
			"## URN\n\n" +
			"DataHub generates a server-side UUID for each assertion. The `urn` and `id` " +
			"attributes are populated after creation and are stable across updates. " +
			"ImportState requires the full assertion URN (e.g. `urn:li:assertion:<uuid>`). " +
			"Only NATIVE (author-as-code) assertions can be imported; ingested EXTERNAL " +
			"(e.g. dbt) or smart/AI INFERRED assertions are refused.",
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
			"sql_type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "SQL assertion sub-type. One of `METRIC` (assert on the " +
					"absolute value the query returns) or `METRIC_CHANGE` (assert on the " +
					"change in that value between evaluations). `METRIC_CHANGE` requires " +
					"`change_type` and a non-empty `description`.",
			},
			"change_type": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "How the metric change is measured: `ABSOLUTE` (a raw delta) " +
					"or `PERCENTAGE` (a percentage change). Required when " +
					"`sql_type = \"METRIC_CHANGE\"`; must be omitted otherwise.",
			},
			"statement": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "SQL SELECT statement that returns a single numeric value to compare against `value`.",
			},
			"operator": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Comparison operator. One of `EQUAL_TO`, `NOT_EQUAL_TO`, `GREATER_THAN`, `GREATER_THAN_OR_EQUAL_TO`, `LESS_THAN`, `LESS_THAN_OR_EQUAL_TO`.",
			},
			"value": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Expected numeric result of the SQL statement (as a string, e.g. `\"0\"`).",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Human-readable description of what this SQL assertion checks.",
			},
			"failure_severity": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Severity raised when this assertion fails: `LOW`, `MEDIUM`, or " +
					"`HIGH`. Omit to use the DataHub default. (Conditional per-result severity " +
					"rules are not modeled.)",
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

// ValidateConfig enforces the change_type/sql_type pairing: change_type is
// required for METRIC_CHANGE and rejected otherwise. METRIC_CHANGE also requires
// a non-empty description (DataHub rejects the mutation without one). Skipped
// while a relevant attribute is unknown at plan time.
func (r *sqlAssertionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg sqlAssertionResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if cfg.SQLType.IsUnknown() || cfg.ChangeType.IsUnknown() {
		return
	}

	isChange := cfg.SQLType.ValueString() == "METRIC_CHANGE"
	hasChangeType := !cfg.ChangeType.IsNull() && cfg.ChangeType.ValueString() != ""

	if isChange && !hasChangeType {
		resp.Diagnostics.AddAttributeError(
			path.Root("change_type"),
			"Missing change_type",
			"change_type is required when sql_type = \"METRIC_CHANGE\" "+
				"(set it to \"ABSOLUTE\" or \"PERCENTAGE\").",
		)
	}
	if !isChange && hasChangeType {
		resp.Diagnostics.AddAttributeError(
			path.Root("change_type"),
			"Unexpected change_type",
			"change_type is only valid when sql_type = \"METRIC_CHANGE\"; "+
				"remove it for METRIC assertions.",
		)
	}
	if isChange && !cfg.Description.IsUnknown() &&
		(cfg.Description.IsNull() || cfg.Description.ValueString() == "") {
		resp.Diagnostics.AddAttributeError(
			path.Root("description"),
			"Missing description",
			"description is required when sql_type = \"METRIC_CHANGE\"; "+
				"DataHub rejects a metric-change assertion without one.",
		)
	}
}

func (r *sqlAssertionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan sqlAssertionResourceModel
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

	urn, err := r.client.UpsertSQLAssertion(ctx, datahub.SQLAssertionInput{
		EntityURN:          plan.EntityURN.ValueString(),
		SQLType:            plan.SQLType.ValueString(),
		ChangeType:         strVal(plan.ChangeType),
		Statement:          plan.Statement.ValueString(),
		Operator:           plan.Operator.ValueString(),
		Value:              plan.Value.ValueString(),
		Description:        strVal(plan.Description),
		FailureSeverity:    strVal(plan.FailureSeverity),
		EvaluationCron:     plan.EvaluationCron.ValueString(),
		EvaluationTimezone: plan.EvaluationTimezone.ValueString(),
		OnSuccessActions:   onSuccess,
		OnFailureActions:   onFailure,
		Mode:               plan.Mode.ValueString(),
		ExecutorID:         strVal(plan.ExecutorID),
	})
	if err != nil {
		if errors.Is(err, datahub.ErrAssertionCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required",
				"datahub_sql_assertion requires DataHub Cloud. "+
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

func (r *sqlAssertionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state sqlAssertionResourceModel
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

	if ai.SQL != nil {
		state.SQLType = types.StringValue(ai.SQL.SQLType)
		state.ChangeType = nullIfEmpty(ai.SQL.ChangeType)
		state.Statement = types.StringValue(ai.SQL.Statement)
		state.Operator = types.StringValue(ai.SQL.Operator)
		state.Value = types.StringValue(ai.SQL.Value)
		state.Description = nullIfEmpty(ai.SQL.Description)
		state.FailureSeverity = nullIfEmpty(ai.FailureSeverity)
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

	// Recover monitor-side fields (evaluation schedule, mode) from the associated
	// Monitor entity so ImportState produces a clean plan. SQL assertions have no
	// source_type.
	mon, err := r.client.GetAssertionMonitor(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if mon != nil {
		state.EvaluationCron = nullIfEmpty(mon.EvaluationCron)
		state.EvaluationTimezone = nullIfEmpty(mon.EvaluationTimezone)
		if mon.Mode != "" {
			state.Mode = types.StringValue(mon.Mode)
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *sqlAssertionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan, state sqlAssertionResourceModel
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

	_, err := r.client.UpsertSQLAssertion(ctx, datahub.SQLAssertionInput{
		AssertionURN:       state.URN.ValueString(),
		EntityURN:          plan.EntityURN.ValueString(),
		SQLType:            plan.SQLType.ValueString(),
		ChangeType:         strVal(plan.ChangeType),
		Statement:          plan.Statement.ValueString(),
		Operator:           plan.Operator.ValueString(),
		Value:              plan.Value.ValueString(),
		Description:        strVal(plan.Description),
		FailureSeverity:    strVal(plan.FailureSeverity),
		EvaluationCron:     plan.EvaluationCron.ValueString(),
		EvaluationTimezone: plan.EvaluationTimezone.ValueString(),
		OnSuccessActions:   onSuccess,
		OnFailureActions:   onFailure,
		Mode:               plan.Mode.ValueString(),
		ExecutorID:         strVal(plan.ExecutorID),
	})
	if err != nil {
		if errors.Is(err, datahub.ErrAssertionCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required",
				"datahub_sql_assertion requires DataHub Cloud.")
			return
		}
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = state.ID
	plan.URN = state.URN
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sqlAssertionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state sqlAssertionResourceModel
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

func (r *sqlAssertionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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

	if ai.SQL == nil {
		resp.Diagnostics.AddError(
			"Wrong assertion type",
			fmt.Sprintf("URN %q is a %q assertion, not a SQL assertion.", raw, ai.Type),
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

	state := sqlAssertionResourceModel{
		ID:                 types.StringValue(ai.URN),
		URN:                types.StringValue(ai.URN),
		EntityURN:          types.StringValue(ai.EntityURN),
		OnSuccessActions:   onSuccess,
		OnFailureActions:   onFailure,
		EvaluationCron:     types.StringValue(""),
		EvaluationTimezone: types.StringValue("UTC"),
		Mode:               types.StringValue("ACTIVE"),
	}
	if ai.SQL != nil {
		state.SQLType = types.StringValue(ai.SQL.SQLType)
		state.ChangeType = nullIfEmpty(ai.SQL.ChangeType)
		state.Statement = types.StringValue(ai.SQL.Statement)
		state.Operator = types.StringValue(ai.SQL.Operator)
		state.Value = types.StringValue(ai.SQL.Value)
		state.Description = nullIfEmpty(ai.SQL.Description)
		state.FailureSeverity = nullIfEmpty(ai.FailureSeverity)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
