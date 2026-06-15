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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ resource.Resource                   = &freshnessAssertionResource{}
	_ resource.ResourceWithConfigure      = &freshnessAssertionResource{}
	_ resource.ResourceWithImportState    = &freshnessAssertionResource{}
	_ resource.ResourceWithValidateConfig = &freshnessAssertionResource{}
)

type freshnessAssertionResource struct {
	client *datahub.Client
}

type freshnessAssertionResourceModel struct {
	ID                    types.String `tfsdk:"id"`
	URN                   types.String `tfsdk:"urn"`
	EntityURN             types.String `tfsdk:"entity_urn"`
	Description           types.String `tfsdk:"description"`
	FilterSQL             types.String `tfsdk:"filter_sql"`
	ScheduleType          types.String `tfsdk:"schedule_type"`
	FixedIntervalUnit     types.String `tfsdk:"fixed_interval_unit"`
	FixedIntervalMultiple types.Int64  `tfsdk:"fixed_interval_multiple"`
	CronSchedule          types.String `tfsdk:"cron_schedule"`
	CronTimezone          types.String `tfsdk:"cron_timezone"`
	EvaluationCron        types.String `tfsdk:"evaluation_cron"`
	EvaluationTimezone    types.String `tfsdk:"evaluation_timezone"`
	SourceType            types.String `tfsdk:"source_type"`
	OnSuccessActions      types.List   `tfsdk:"on_success_actions"`
	OnFailureActions      types.List   `tfsdk:"on_failure_actions"`
	Mode                  types.String `tfsdk:"mode"`
	ExecutorID            types.String `tfsdk:"executor_id"`
}

// NewFreshnessAssertionResource returns a new datahub_freshness_assertion resource.
func NewFreshnessAssertionResource() resource.Resource {
	return &freshnessAssertionResource{}
}

func (r *freshnessAssertionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *freshnessAssertionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_freshness_assertion"
}

func (r *freshnessAssertionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: cloudOnlyBadge +
			"Creates and manages a DataHub freshness assertion monitor on a dataset.\n\n" +
			"Freshness assertions check that a dataset has been updated within an expected " +
			"window (e.g. within the last 24 hours). DataHub evaluates whether a new batch " +
			"of data arrived within the configured schedule window.\n\n" +
			"## Schedule types\n\n" +
			"Set `schedule_type` to `FIXED_INTERVAL` and supply `fixed_interval_unit` / " +
			"`fixed_interval_multiple` for a rolling window (e.g. data must arrive every 1 day). " +
			"Set `schedule_type` to `CRON` and supply `cron_schedule` / `cron_timezone` for " +
			"a calendar-based window. Set `schedule_type` to `SINCE_THE_LAST_CHECK` to require " +
			"that the dataset changed at all between consecutive evaluations; this type takes " +
			"no window sub-configuration.\n\n" +
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
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Human-readable description of what this freshness assertion checks.",
			},
			"filter_sql": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Optional SQL `WHERE` clause (without the `WHERE` keyword) " +
					"restricting which rows the freshness check considers, e.g. `\"region = 'EU'\"`.",
			},
			"schedule_type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Freshness window type: `FIXED_INTERVAL` (rolling window), " +
					"`CRON` (calendar window), or `SINCE_THE_LAST_CHECK` (any change since the " +
					"previous evaluation; no window sub-configuration).",
			},
			"fixed_interval_unit": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Time unit for a fixed-interval schedule: `HOUR`, `DAY`, `WEEK`, `MONTH`, or `YEAR`. Required when `schedule_type = \"FIXED_INTERVAL\"`.",
			},
			"fixed_interval_multiple": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Number of units in the fixed interval (e.g. `24` for 24 hours). Required when `schedule_type = \"FIXED_INTERVAL\"`.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"cron_schedule": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Cron expression defining the freshness window (for `schedule_type = \"CRON\"`).",
			},
			"cron_timezone": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Timezone for the cron window schedule (e.g. `\"UTC\"`).",
			},
			"evaluation_cron": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Cron expression defining when DataHub evaluates this assertion.",
			},
			"evaluation_timezone": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Timezone for the evaluation cron schedule (e.g. `\"UTC\"`).",
			},
			"source_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "How DataHub determines freshness. `AUDIT_LOG` uses the platform audit log. `INFORMATION_SCHEMA` queries the source catalog.",
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

// ValidateConfig enforces that the window sub-fields match schedule_type:
// FIXED_INTERVAL needs fixed_interval_unit/multiple and rejects the cron fields;
// CRON needs cron_schedule/cron_timezone and rejects the fixed-interval fields;
// SINCE_THE_LAST_CHECK takes no window sub-configuration. Skipped while
// schedule_type is unknown at plan time.
func (r *freshnessAssertionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg freshnessAssertionResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() || cfg.ScheduleType.IsUnknown() {
		return
	}

	hasUnit := !cfg.FixedIntervalUnit.IsNull() && cfg.FixedIntervalUnit.ValueString() != ""
	hasMultiple := !cfg.FixedIntervalMultiple.IsNull()
	hasCron := !cfg.CronSchedule.IsNull() && cfg.CronSchedule.ValueString() != ""
	hasCronTZ := !cfg.CronTimezone.IsNull() && cfg.CronTimezone.ValueString() != ""

	reject := func(p string, msg string) {
		resp.Diagnostics.AddAttributeError(path.Root(p), "Unexpected attribute", msg)
	}

	switch cfg.ScheduleType.ValueString() {
	case "FIXED_INTERVAL":
		if !hasUnit {
			resp.Diagnostics.AddAttributeError(path.Root("fixed_interval_unit"), "Missing fixed_interval_unit",
				"fixed_interval_unit is required when schedule_type = \"FIXED_INTERVAL\".")
		}
		if !hasMultiple {
			resp.Diagnostics.AddAttributeError(path.Root("fixed_interval_multiple"), "Missing fixed_interval_multiple",
				"fixed_interval_multiple is required when schedule_type = \"FIXED_INTERVAL\".")
		}
		if hasCron {
			reject("cron_schedule", "cron_schedule is only valid when schedule_type = \"CRON\".")
		}
		if hasCronTZ {
			reject("cron_timezone", "cron_timezone is only valid when schedule_type = \"CRON\".")
		}
	case "CRON":
		if !hasCron {
			resp.Diagnostics.AddAttributeError(path.Root("cron_schedule"), "Missing cron_schedule",
				"cron_schedule is required when schedule_type = \"CRON\".")
		}
		if !hasCronTZ {
			resp.Diagnostics.AddAttributeError(path.Root("cron_timezone"), "Missing cron_timezone",
				"cron_timezone is required when schedule_type = \"CRON\".")
		}
		if hasUnit {
			reject("fixed_interval_unit", "fixed_interval_unit is only valid when schedule_type = \"FIXED_INTERVAL\".")
		}
		if hasMultiple {
			reject("fixed_interval_multiple", "fixed_interval_multiple is only valid when schedule_type = \"FIXED_INTERVAL\".")
		}
	case "SINCE_THE_LAST_CHECK":
		if hasUnit {
			reject("fixed_interval_unit", "fixed_interval_unit is not valid for schedule_type = \"SINCE_THE_LAST_CHECK\".")
		}
		if hasMultiple {
			reject("fixed_interval_multiple", "fixed_interval_multiple is not valid for schedule_type = \"SINCE_THE_LAST_CHECK\".")
		}
		if hasCron {
			reject("cron_schedule", "cron_schedule is not valid for schedule_type = \"SINCE_THE_LAST_CHECK\".")
		}
		if hasCronTZ {
			reject("cron_timezone", "cron_timezone is not valid for schedule_type = \"SINCE_THE_LAST_CHECK\".")
		}
	}
}

func (r *freshnessAssertionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan freshnessAssertionResourceModel
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

	urn, err := r.client.UpsertFreshnessAssertion(ctx, datahub.FreshnessAssertionInput{
		EntityURN:             plan.EntityURN.ValueString(),
		Description:           strVal(plan.Description),
		FilterSQL:             strVal(plan.FilterSQL),
		ScheduleType:          plan.ScheduleType.ValueString(),
		FixedIntervalUnit:     strVal(plan.FixedIntervalUnit),
		FixedIntervalMultiple: plan.FixedIntervalMultiple.ValueInt64(),
		CronSchedule:          strVal(plan.CronSchedule),
		CronTimezone:          strVal(plan.CronTimezone),
		EvaluationCron:        plan.EvaluationCron.ValueString(),
		EvaluationTimezone:    plan.EvaluationTimezone.ValueString(),
		SourceType:            plan.SourceType.ValueString(),
		OnSuccessActions:      onSuccess,
		OnFailureActions:      onFailure,
		Mode:                  plan.Mode.ValueString(),
		ExecutorID:            strVal(plan.ExecutorID),
	})
	if err != nil {
		if errors.Is(err, datahub.ErrAssertionCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required",
				"datahub_freshness_assertion requires DataHub Cloud. "+
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

func (r *freshnessAssertionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state freshnessAssertionResourceModel
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

	// Merge readable fields from assertionInfo; preserve monitor-only state fields.
	if ai.Freshness != nil {
		state.ScheduleType = types.StringValue(ai.Freshness.ScheduleType)
		if ai.Freshness.ScheduleType == "FIXED_INTERVAL" {
			state.FixedIntervalUnit = nullIfEmpty(ai.Freshness.FixedIntervalUnit)
			state.FixedIntervalMultiple = types.Int64Value(ai.Freshness.FixedIntervalMultiple)
		}
		if ai.Freshness.ScheduleType == "CRON" {
			state.CronSchedule = nullIfEmpty(ai.Freshness.CronSchedule)
			state.CronTimezone = nullIfEmpty(ai.Freshness.CronTimezone)
		}
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

	// Recover the monitor-side fields (evaluation schedule, source type, mode)
	// from the associated Monitor entity. Without this, ImportState leaves these
	// required/optional fields empty and the first plan shows a spurious diff.
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

func (r *freshnessAssertionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan, state freshnessAssertionResourceModel
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

	_, err := r.client.UpsertFreshnessAssertion(ctx, datahub.FreshnessAssertionInput{
		AssertionURN:          state.URN.ValueString(),
		EntityURN:             plan.EntityURN.ValueString(),
		Description:           strVal(plan.Description),
		FilterSQL:             strVal(plan.FilterSQL),
		ScheduleType:          plan.ScheduleType.ValueString(),
		FixedIntervalUnit:     strVal(plan.FixedIntervalUnit),
		FixedIntervalMultiple: plan.FixedIntervalMultiple.ValueInt64(),
		CronSchedule:          strVal(plan.CronSchedule),
		CronTimezone:          strVal(plan.CronTimezone),
		EvaluationCron:        plan.EvaluationCron.ValueString(),
		EvaluationTimezone:    plan.EvaluationTimezone.ValueString(),
		SourceType:            plan.SourceType.ValueString(),
		OnSuccessActions:      onSuccess,
		OnFailureActions:      onFailure,
		Mode:                  plan.Mode.ValueString(),
		ExecutorID:            strVal(plan.ExecutorID),
	})
	if err != nil {
		if errors.Is(err, datahub.ErrAssertionCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required",
				"datahub_freshness_assertion requires DataHub Cloud.")
			return
		}
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = state.ID
	plan.URN = state.URN
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *freshnessAssertionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state freshnessAssertionResourceModel
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

func (r *freshnessAssertionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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

	if ai.Freshness == nil {
		resp.Diagnostics.AddError(
			"Wrong assertion type",
			fmt.Sprintf("URN %q is a %q assertion, not a freshness assertion.", raw, ai.Type),
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

	state := freshnessAssertionResourceModel{
		ID:                 types.StringValue(ai.URN),
		URN:                types.StringValue(ai.URN),
		EntityURN:          types.StringValue(ai.EntityURN),
		Description:        nullIfEmpty(ai.Description),
		FilterSQL:          nullIfEmpty(ai.FilterSQL),
		OnSuccessActions:   onSuccess,
		OnFailureActions:   onFailure,
		EvaluationCron:     types.StringValue(""),
		EvaluationTimezone: types.StringValue("UTC"),
		SourceType:         types.StringValue("AUDIT_LOG"),
		Mode:               types.StringValue("ACTIVE"),
	}
	if ai.Freshness != nil {
		state.ScheduleType = types.StringValue(ai.Freshness.ScheduleType)
		// Only set the window sub-fields belonging to the schedule type; the others
		// stay null so SINCE_THE_LAST_CHECK (and the unused side of FIXED_INTERVAL /
		// CRON) imports without a spurious diff against config.
		switch ai.Freshness.ScheduleType {
		case "FIXED_INTERVAL":
			state.FixedIntervalUnit = nullIfEmpty(ai.Freshness.FixedIntervalUnit)
			state.FixedIntervalMultiple = types.Int64Value(ai.Freshness.FixedIntervalMultiple)
		case "CRON":
			state.CronSchedule = nullIfEmpty(ai.Freshness.CronSchedule)
			state.CronTimezone = nullIfEmpty(ai.Freshness.CronTimezone)
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
