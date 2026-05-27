// Copyright 2026 The DataHub Project Authors
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/tools/uid"
)

var (
	_ resource.Resource                = &ingestionSourceResource{}
	_ resource.ResourceWithConfigure   = &ingestionSourceResource{}
	_ resource.ResourceWithImportState = &ingestionSourceResource{}
)

type ingestionSourceResource struct {
	client *datahub.Client
}

type ingestionSourceResourceModel struct {
	ID               types.String `tfsdk:"id"`
	SourceID         types.String `tfsdk:"source_id"`
	SourceName       types.String `tfsdk:"source_name"`
	SourceType       types.String `tfsdk:"source_type"`
	RemoteExecutorID types.String `tfsdk:"remote_executor_id"`
	CronInterval     types.String `tfsdk:"cron_interval"`
	Timezone         types.String `tfsdk:"timezone"`
	CLIVersion       types.String `tfsdk:"cli_version"`
	ExtraArgs        types.Map    `tfsdk:"extra_args"`
	Recipe           types.String `tfsdk:"recipe"`
	DebugMode        types.Bool   `tfsdk:"debug_mode"`
	Platform         types.String `tfsdk:"platform"`
}

type createOrUpdateResult struct {
	SourceID         types.String
	SourceType       string
	RemoteExecutorID types.String
	CronInterval     types.String
	Timezone         types.String
	CLIVersion       types.String
	ExtraArgs        types.Map
	DebugMode        types.Bool
	Platform         types.String
}

func NewIngestionSourceResource() resource.Resource {
	return &ingestionSourceResource{}
}

func (r *ingestionSourceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ingestionSourceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ingestion_source"
}

func (r *ingestionSourceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a DataHub Ingestion Source using a raw recipe JSON string.\n\n" +
			"This is similar in spirit to `aws_iam_policy`: the resource stores a JSON document (the recipe) in the target system (DataHub).\n\n" +
			"## Argument Reference\n\n" +
			"- `source_id` (Optional) Unique id for the ingestion source. If omitted, it is derived from `source_name` as `<sanitized-source_name>-<hash>`. This becomes the Terraform resource id.\n" +
			"- `source_name` (Required) Human-friendly name shown in the DataHub UI.\n" +
			"- `recipe` (Required) Recipe JSON string. Build it with `jsonencode({...})` or any mechanism that produces valid JSON.\n" +
			"- `cron_interval` (Optional) Cron schedule expression (e.g. `0 10 * * *`). If omitted, no schedule is sent.\n" +
			"- `timezone` (Optional) Schedule timezone. If `cron_interval` is set and timezone is omitted, `UTC` is used.\n" +
			"- `cli_version` (Optional) DataHub ingestion CLI version used by DataHub to execute the source. If omitted, it is not sent.\n" +
			"- `extra_args` (Optional) Extra arguments sent to DataHub as `config.extraArgs` (map of string keys to string values). For example, set `extra_pip_requirements` to add pip deps.\n" +
			"- `debug_mode` (Optional) Enable debug mode for this ingestion source (`config.debugMode`). Produces verbose executor logs for troubleshooting.\n" +
			"- `platform` (Optional) DataPlatform URN to associate with this ingestion source (e.g. `urn:li:dataPlatform:bigquery`). Used by the DataHub UI for icons and filtering.\n" +
			"- `source_type` (Optional) Ingestion source type. If omitted, it is derived from `recipe.source.type`. If set, it must match the type inside the recipe.\n\n" +
			"## Security Note\n\n" +
			"**Warning:** The recipe content is stored in DataHub as part of the Ingestion Source configuration. If you embed credentials directly in the recipe JSON, they can be stored in DataHub and may be visible to users/services with access to ingestion source configurations.\n\n" +
			"**Recommended:** Use DataHub Secrets / environment variable substitution (e.g. `${SECRET_NAME}`) instead of hard-coded credentials.\n\n" +
			"References: https://docs.datahub.com/docs/ui-ingestion/#configuring-secrets and https://docs.datahub.com/docs/metadata-ingestion/recipe_overview#loading-sensitive-data-as-files-in-recipes.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"source_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Unique id for the ingestion source (DataHub identifier). If omitted, it is derived from `source_name` as `<sanitized-source_name>-<hash>`. This is also used as the Terraform resource id.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"source_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-friendly name for the ingestion source as shown in the DataHub UI. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"remote_executor_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Optional remote executor id (DataHub `config.executorId`). If omitted, it is not sent and will be omitted from the stored ingestion source config.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"source_type": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Ingestion source type. If omitted, it is derived from `recipe.source.type`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"cron_interval": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Optional cron schedule expression for the ingestion source. If omitted, no schedule is sent.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"timezone": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Optional timezone for the schedule (e.g. `UTC`). If `cron_interval` is set and timezone is omitted, `UTC` is used.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"cli_version": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Optional DataHub ingestion CLI version used by DataHub to execute the source. If omitted, it is not sent.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"extra_args": schema.MapAttribute{
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Optional extra arguments sent to DataHub as `config.extraArgs` (map of string keys to string values). This can be used for settings like `extra_pip_requirements`.",
				PlanModifiers:       []planmodifier.Map{mapplanmodifier.UseStateForUnknown()},
			},
			"recipe": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Recipe JSON string. Avoid embedding secrets directly; prefer `${SECRET_NAME}` / `${ENV_VAR}` placeholders so DataHub can resolve credentials via Secrets or environment variables.",
			},
			"debug_mode": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Whether to run this ingestion source in debug mode (DataHub `config.debugMode`). Enables verbose logging in the ingestion executor for troubleshooting failing runs.",
			},
			"platform": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Optional DataPlatform URN to associate with this ingestion source (e.g. `urn:li:dataPlatform:bigquery`). Used by the DataHub UI for icons and filtering.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *ingestionSourceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration (host, gms_token) is set.")
		return
	}

	var plan ingestionSourceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, diags := r.createOrUpdate(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.SourceID = result.SourceID
	plan.ID = result.SourceID
	plan.SourceType = types.StringValue(result.SourceType)
	plan.RemoteExecutorID = result.RemoteExecutorID
	plan.CronInterval = result.CronInterval
	plan.Timezone = result.Timezone
	plan.CLIVersion = result.CLIVersion
	plan.ExtraArgs = result.ExtraArgs
	plan.DebugMode = result.DebugMode
	plan.Platform = result.Platform

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ingestionSourceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration (host, gms_token) is set.")
		return
	}

	var state ingestionSourceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sourceID := strings.TrimSpace(state.SourceID.ValueString())
	if sourceID == "" {
		sourceID = strings.TrimSpace(state.ID.ValueString())
	}
	if sourceID == "" {
		resp.Diagnostics.AddError("Invalid state", "Missing source_id/id in state; cannot read remote ingestion source.")
		return
	}

	body, err := r.client.GetIngestionSourceByID(ctx, sourceID)
	if err != nil {
		if errors.Is(err, datahub.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Datahub API Error", err.Error())
		return
	}

	state.ID = types.StringValue(sourceID)
	state.SourceID = types.StringValue(sourceID)

	var remote datahub.IngestionSource
	if err := json.Unmarshal(body, &remote); err != nil {
		resp.Diagnostics.AddError("Invalid API response", fmt.Sprintf("Failed to parse ingestion source response JSON: %v", err))
		return
	}

	if remote.DataHubIngestionSourceInfo.Value.Name != "" {
		state.SourceName = types.StringValue(remote.DataHubIngestionSourceInfo.Value.Name)
	}
	if remote.DataHubIngestionSourceInfo.Value.Type != "" {
		state.SourceType = types.StringValue(remote.DataHubIngestionSourceInfo.Value.Type)
	}
	if remote.DataHubIngestionSourceInfo.Value.Schedule != nil {
		if interval := strings.TrimSpace(remote.DataHubIngestionSourceInfo.Value.Schedule.Interval); interval != "" {
			state.CronInterval = types.StringValue(interval)
		} else {
			state.CronInterval = types.StringNull()
		}
		if tz := strings.TrimSpace(remote.DataHubIngestionSourceInfo.Value.Schedule.Timezone); tz != "" {
			state.Timezone = types.StringValue(tz)
		} else {
			state.Timezone = types.StringNull()
		}
	} else {
		state.CronInterval = types.StringNull()
		state.Timezone = types.StringNull()
	}
	if version := strings.TrimSpace(remote.DataHubIngestionSourceInfo.Value.Config.Version); version != "" {
		state.CLIVersion = types.StringValue(version)
	} else {
		state.CLIVersion = types.StringNull()
	}
	if execID := strings.TrimSpace(remote.DataHubIngestionSourceInfo.Value.Config.ExecutorID); execID != "" {
		state.RemoteExecutorID = types.StringValue(execID)
	} else {
		state.RemoteExecutorID = types.StringNull()
	}

	if len(remote.DataHubIngestionSourceInfo.Value.Config.ExtraArgs) > 0 {
		elems := make(map[string]attr.Value, len(remote.DataHubIngestionSourceInfo.Value.Config.ExtraArgs))
		for k, v := range remote.DataHubIngestionSourceInfo.Value.Config.ExtraArgs {
			elems[k] = types.StringValue(v)
		}
		mv, d := types.MapValue(types.StringType, elems)
		resp.Diagnostics.Append(d...)
		if !resp.Diagnostics.HasError() {
			state.ExtraArgs = mv
		}
	} else {
		state.ExtraArgs = types.MapNull(types.StringType)
	}

	if remoteRecipe := strings.TrimSpace(remote.DataHubIngestionSourceInfo.Value.Config.Recipe); remoteRecipe != "" {
		state.Recipe = types.StringValue(remoteRecipe)
	}

	if remote.DataHubIngestionSourceInfo.Value.Config.DebugMode != nil {
		state.DebugMode = types.BoolValue(*remote.DataHubIngestionSourceInfo.Value.Config.DebugMode)
	} else {
		state.DebugMode = types.BoolNull()
	}

	if p := strings.TrimSpace(remote.DataHubIngestionSourceInfo.Value.Platform); p != "" {
		state.Platform = types.StringValue(p)
	} else {
		state.Platform = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ingestionSourceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration (host, gms_token) is set.")
		return
	}

	var plan ingestionSourceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, diags := r.createOrUpdate(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.SourceID = result.SourceID
	plan.ID = result.SourceID
	plan.SourceType = types.StringValue(result.SourceType)
	plan.RemoteExecutorID = result.RemoteExecutorID
	plan.CronInterval = result.CronInterval
	plan.Timezone = result.Timezone
	plan.CLIVersion = result.CLIVersion
	plan.ExtraArgs = result.ExtraArgs
	plan.DebugMode = result.DebugMode
	plan.Platform = result.Platform

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ingestionSourceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration (host, gms_token) is set.")
		return
	}

	var state ingestionSourceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sourceID := strings.TrimSpace(state.SourceID.ValueString())
	if sourceID == "" {
		sourceID = strings.TrimSpace(state.ID.ValueString())
	}
	if sourceID == "" {
		resp.State.RemoveResource(ctx)
		return
	}

	err := r.client.DeleteIngestionSourceByID(ctx, sourceID)
	if err != nil && !errors.Is(err, datahub.ErrNotFound) {
		resp.Diagnostics.AddError("Datahub API Error", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *ingestionSourceResource) createOrUpdate(ctx context.Context, plan ingestionSourceResourceModel) (createOrUpdateResult, diag.Diagnostics) {
	var diags diag.Diagnostics

	sourceID := strings.TrimSpace(plan.SourceID.ValueString())
	sourceName := strings.TrimSpace(plan.SourceName.ValueString())
	cronInterval := strings.TrimSpace(plan.CronInterval.ValueString())
	timezone := strings.TrimSpace(plan.Timezone.ValueString())
	cliVersion := strings.TrimSpace(plan.CLIVersion.ValueString())
	recipe := strings.TrimSpace(plan.Recipe.ValueString())
	remoteExecutorID := strings.TrimSpace(plan.RemoteExecutorID.ValueString())

	var extraArgs map[string]string
	effectiveExtraArgs := types.MapNull(types.StringType)
	if !plan.ExtraArgs.IsNull() && !plan.ExtraArgs.IsUnknown() {
		elems := plan.ExtraArgs.Elements()
		if len(elems) > 0 {
			extraArgs = make(map[string]string, len(elems))
			for k, v := range elems {
				sv, ok := v.(types.String)
				if !ok {
					diags.AddError("Invalid extra_args", fmt.Sprintf("extra_args[%q] must be a string", k))
					return createOrUpdateResult{}, diags
				}
				if sv.IsUnknown() {
					diags.AddError("Invalid extra_args", fmt.Sprintf("extra_args[%q] is unknown", k))
					return createOrUpdateResult{}, diags
				}
				if sv.IsNull() {
					continue
				}
				extraArgs[k] = sv.ValueString()
			}
			if len(extraArgs) > 0 {
				effectiveElems := make(map[string]attr.Value, len(extraArgs))
				for k, v := range extraArgs {
					effectiveElems[k] = types.StringValue(v)
				}
				mv, d := types.MapValue(types.StringType, effectiveElems)
				diags.Append(d...)
				if diags.HasError() {
					return createOrUpdateResult{}, diags
				}
				effectiveExtraArgs = mv
			}
		}
	}

	if sourceName == "" {
		diags.AddError("Invalid plan", "source_name is required")
		return createOrUpdateResult{}, diags
	}
	if sourceID == "" || plan.SourceID.IsNull() || plan.SourceID.IsUnknown() {
		sourceID = uid.DeriveID(sourceName, []byte(sourceName), 48)
	}
	if recipe == "" {
		diags.AddError("Invalid plan", "recipe must be a non-empty JSON string")
		return createOrUpdateResult{}, diags
	}

	effectiveSourceID := types.StringValue(sourceID)

	var doc struct {
		Source struct {
			Type string `json:"type"`
		} `json:"source"`
	}
	if err := json.Unmarshal([]byte(recipe), &doc); err != nil {
		diags.AddError("Invalid recipe JSON", fmt.Sprintf("recipe must be valid JSON: %v", err))
		return createOrUpdateResult{}, diags
	}

	recipeSourceType := strings.TrimSpace(doc.Source.Type)
	sourceType := strings.TrimSpace(plan.SourceType.ValueString())
	if sourceType == "" {
		sourceType = recipeSourceType
	}
	if sourceType == "" {
		diags.AddError("Missing source_type", "source_type must be set or present at recipe.source.type")
		return createOrUpdateResult{}, diags
	}
	if recipeSourceType != "" && sourceType != recipeSourceType {
		diags.AddError(
			"source_type mismatch",
			fmt.Sprintf("source_type (%q) does not match recipe.source.type (%q)", sourceType, recipeSourceType),
		)
		return createOrUpdateResult{}, diags
	}

	var executorIDPtr *string
	effectiveRemoteExecutorID := types.StringNull()
	if !plan.RemoteExecutorID.IsNull() && !plan.RemoteExecutorID.IsUnknown() {
		if remoteExecutorID != "" {
			executorIDPtr = &remoteExecutorID
			effectiveRemoteExecutorID = types.StringValue(remoteExecutorID)
		}
	}

	var cronIntervalPtr *string
	effectiveCronInterval := types.StringNull()
	if !plan.CronInterval.IsNull() && !plan.CronInterval.IsUnknown() {
		if cronInterval != "" {
			cronIntervalPtr = &cronInterval
			effectiveCronInterval = types.StringValue(cronInterval)
		}
	}

	var timezonePtr *string
	effectiveTimezone := types.StringNull()
	if cronIntervalPtr != nil {
		if !plan.Timezone.IsNull() && !plan.Timezone.IsUnknown() {
			if timezone != "" {
				timezonePtr = &timezone
			}
		}
		if timezonePtr == nil {
			utc := "UTC"
			timezonePtr = &utc
		}
		effectiveTimezone = types.StringValue(*timezonePtr)
	}

	var cliVersionPtr *string
	effectiveCLIVersion := types.StringNull()
	if !plan.CLIVersion.IsNull() && !plan.CLIVersion.IsUnknown() {
		if cliVersion != "" {
			cliVersionPtr = &cliVersion
			effectiveCLIVersion = types.StringValue(cliVersion)
		}
	}

	var debugModePtr *bool
	effectiveDebugMode := types.BoolNull()
	if !plan.DebugMode.IsNull() && !plan.DebugMode.IsUnknown() {
		v := plan.DebugMode.ValueBool()
		debugModePtr = &v
		effectiveDebugMode = types.BoolValue(v)
	}

	var platformPtr *string
	effectivePlatform := types.StringNull()
	if !plan.Platform.IsNull() && !plan.Platform.IsUnknown() {
		p := strings.TrimSpace(plan.Platform.ValueString())
		if p != "" {
			platformPtr = &p
			effectivePlatform = types.StringValue(p)
		}
	}

	respBody, err := r.client.NewDatasourceIngestion(ctx, datahub.DatasourceIngestionInput{
		SourceID:     sourceID,
		SourceName:   sourceName,
		SourceType:   sourceType,
		ExtraArgs:    extraArgs,
		ExecutorID:   executorIDPtr,
		CronInterval: cronIntervalPtr,
		Timezone:     timezonePtr,
		CLIVersion:   cliVersionPtr,
		RecipeJSON:   &recipe,
		DebugMode:    debugModePtr,
		Platform:     platformPtr,
	})
	if err != nil {
		diags.AddError("Datahub API Error", err.Error())
		return createOrUpdateResult{}, diags
	}

	tflog.Debug(ctx, "DataHub ingestion source upsert response", map[string]any{
		"source_id": sourceID,
		"body":      string(respBody),
	})

	return createOrUpdateResult{
		SourceID:         effectiveSourceID,
		SourceType:       sourceType,
		RemoteExecutorID: effectiveRemoteExecutorID,
		CronInterval:     effectiveCronInterval,
		Timezone:         effectiveTimezone,
		CLIVersion:       effectiveCLIVersion,
		ExtraArgs:        effectiveExtraArgs,
		DebugMode:        effectiveDebugMode,
		Platform:         effectivePlatform,
	}, diags
}

func (r *ingestionSourceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub ingestion source URN (e.g. urn:li:dataHubIngestionSource:my-source) or a bare source_id.")
		return
	}

	const urnPrefix = "urn:li:dataHubIngestionSource:"
	var sourceID string
	if strings.HasPrefix(raw, urnPrefix) {
		sourceID = strings.TrimPrefix(raw, urnPrefix)
	} else {
		sourceID = raw
	}
	if sourceID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Could not extract a source_id from the provided import ID.")
		return
	}

	// Set the two ID fields the Read method needs to perform its lookup.
	// The framework calls Read after ImportState to populate all remaining fields.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), sourceID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("source_id"), sourceID)...)
}
