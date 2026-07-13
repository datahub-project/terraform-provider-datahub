// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/tools/uid"
)

var (
	_ resource.Resource                = &actionPipelineResource{}
	_ resource.ResourceWithConfigure   = &actionPipelineResource{}
	_ resource.ResourceWithImportState = &actionPipelineResource{}
)

type actionPipelineResource struct {
	client *datahub.Client
}

type actionPipelineResourceModel struct {
	ID          types.String         `tfsdk:"id"`
	URN         types.String         `tfsdk:"urn"`
	ActionID    types.String         `tfsdk:"action_id"`
	Name        types.String         `tfsdk:"name"`
	Type        types.String         `tfsdk:"type"`
	Category    types.String         `tfsdk:"category"`
	Description types.String         `tfsdk:"description"`
	Recipe      jsontypes.Normalized `tfsdk:"recipe"`
	ExecutorID  types.String         `tfsdk:"executor_id"`
	Version     types.String         `tfsdk:"version"`
	DebugMode   types.Bool           `tfsdk:"debug_mode"`
}

// NewActionPipelineResource returns a new datahub_action_pipeline resource.
func NewActionPipelineResource() resource.Resource {
	return &actionPipelineResource{}
}

func (r *actionPipelineResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	client := pd.Client
	r.client = client
}

func (r *actionPipelineResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_action_pipeline"
}

func (r *actionPipelineResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: cloudOnlyBadge +
			"Creates and manages a DataHub Cloud **action pipeline** (automation) -- a packaged " +
			"action that runs a recipe to propagate metadata (descriptions, tags, glossary terms) " +
			"back to a platform such as BigQuery or Dataplex.\n\n" +
			"Action pipelines are a newer DataHub Cloud capability. DataHub Cloud upgrades on its own " +
			"release cadence, so a release may occasionally affect this resource; fixes are handled in the " +
			"provider. Pin the provider version for client-side stability and upgrade it to pick up fixes " +
			"(including any needed for backend changes), and please open an issue if you hit one.\n\n" +
			"This resource manages the pipeline **definition** (name, type, recipe, executor). It does not " +
			"model run state (running/stopped); a freshly created pipeline is started by DataHub.\n\n" +
			"## Argument Reference\n\n" +
			"- `action_id` (Optional) Unique id (URN suffix). If omitted, derived from `name` as `<sanitized-name>-<hash>`. Changing it forces a new resource.\n" +
			"- `name` (Required) Human-friendly name shown in the DataHub UI.\n" +
			"- `type` (Required) Action class string, e.g. `dataplex_metadata_sync` or `datahub_integrations.propagation.bigquery.tag_propagator.BigqueryTagPropagatorAction`. The set is open and Cloud-version-dependent; not validated against an enum.\n" +
			"- `recipe` (Required) Action recipe as a JSON string. Build it with `jsonencode({...})`.\n" +
			"- `category` (Optional) UI grouping category (e.g. `Data Discovery`).\n" +
			"- `description` (Optional) Human-readable description.\n" +
			"- `executor_id` (Optional) Executor that runs the action (e.g. `default`).\n" +
			"- `version` (Optional) Action package version.\n" +
			"- `debug_mode` (Optional) Enable verbose executor logging.\n\n" +
			"## Security Note\n\n" +
			"**Warning:** The recipe is stored in DataHub. Do not embed credentials directly.\n\n" +
			"**Recommended:** Use DataHub Secrets / environment variable substitution (e.g. `${SECRET_NAME}`); placeholders are stored verbatim and resolved at execution time.\n\n" +
			"References: https://docs.datahub.com/docs/ui-ingestion/#configuring-secrets and https://docs.datahub.com/docs/metadata-ingestion/recipe_overview#loading-sensitive-data-as-files-in-recipes.\n\n" +
			"## URN\n\n" +
			"The URN is `urn:li:dataHubAction:<action_id>` (deterministic). ImportState accepts either the full URN or a bare `action_id`, so existing UUID-suffixed pipelines (created via the UI or other tooling) can be adopted.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Terraform resource id; equal to `action_id`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN (`urn:li:dataHubAction:<action_id>`).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"action_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Unique id (URN suffix). If omitted, derived from `name` as `<sanitized-name>-<hash>`. Changing it forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-friendly name shown in the DataHub UI.",
			},
			"type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Action class string (e.g. `dataplex_metadata_sync`). Open set; not enum-validated.",
			},
			"category": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "UI grouping category (e.g. `Data Discovery`).",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Human-readable description of what this action pipeline does.",
			},
			"recipe": schema.StringAttribute{
				Required:            true,
				CustomType:          jsontypes.NormalizedType{},
				MarkdownDescription: "Action recipe JSON string. Avoid embedding secrets; prefer `${SECRET_NAME}` placeholders. Compared by JSON semantic equality, so formatting/key-order differences do not produce spurious plan diffs.",
			},
			"executor_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Executor that runs the action (e.g. `default`). If omitted, DataHub's default is used.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"version": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Action package version. If omitted, not sent.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"debug_mode": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Enable verbose executor logging for troubleshooting.",
			},
		},
	}
}

func (r *actionPipelineResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var plan actionPipelineResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.upsert(ctx, &plan, "", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *actionPipelineResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var plan, state actionPipelineResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// action_id is RequiresReplace, so it is stable across an update; reuse it.
	r.upsert(ctx, &plan, state.ActionID.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// upsert builds the client input from plan, calls UpsertActionPipeline, and
// fills the computed plan fields (id/urn/action_id + effective optionals). On a
// non-cloud-only upsert error it verifies via GetActionPipelineByID: if the
// definition persisted (the upsert wrote metadata but the runtime reload failed),
// it treats the create as a success with a warning; otherwise it surfaces the error.
// existingID is empty on Create and the prior action_id on Update.
func (r *actionPipelineResource) upsert(ctx context.Context, plan *actionPipelineResourceModel, existingID string, diags *diag.Diagnostics) {
	name := strings.TrimSpace(plan.Name.ValueString())
	if name == "" {
		diags.AddError("Invalid plan", "name is required")
		return
	}
	typ := strings.TrimSpace(plan.Type.ValueString())
	if typ == "" {
		diags.AddError("Invalid plan", "type is required")
		return
	}
	recipe := strings.TrimSpace(plan.Recipe.ValueString())
	if recipe == "" {
		diags.AddError("Invalid plan", "recipe must be a non-empty JSON string")
		return
	}
	if !json.Valid([]byte(recipe)) {
		diags.AddError("Invalid recipe JSON", "recipe must be valid JSON")
		return
	}

	actionID := strings.TrimSpace(existingID)
	if actionID == "" {
		actionID = strings.TrimSpace(plan.ActionID.ValueString())
	}
	if actionID == "" || plan.ActionID.IsNull() || plan.ActionID.IsUnknown() {
		if existingID != "" {
			actionID = existingID
		} else {
			actionID = uid.DeriveID(name, []byte(name), 48)
		}
	}

	in := datahub.ActionPipelineInput{
		ActionID:    actionID,
		Name:        name,
		Type:        typ,
		Category:    strVal(plan.Category),
		Description: strVal(plan.Description),
		Recipe:      recipe,
		ExecutorID:  strVal(plan.ExecutorID),
		Version:     strVal(plan.Version),
	}
	if !plan.DebugMode.IsNull() && !plan.DebugMode.IsUnknown() {
		v := plan.DebugMode.ValueBool()
		in.DebugMode = &v
	}

	urn, err := r.client.UpsertActionPipeline(ctx, in)
	if err != nil {
		if errors.Is(err, datahub.ErrActionPipelineCloudOnly) {
			diags.AddError("DataHub Cloud Required",
				"datahub_action_pipeline requires DataHub Cloud. "+
					"The configured DataHub instance does not expose action pipeline management.")
			return
		}
		// The upsert may have written the definition before failing to start the
		// runtime. If the entity now exists, treat the definition as saved.
		info, getErr := r.client.GetActionPipelineByID(ctx, actionID)
		if getErr != nil || info == nil {
			diags.AddError("DataHub API Error", err.Error())
			return
		}
		diags.AddWarning("Action pipeline runtime did not start",
			fmt.Sprintf("The action pipeline definition %q was saved, but DataHub reported an error starting it: %s. "+
				"Verify the recipe and executor; the definition is under Terraform management.", actionID, err.Error()))
	}

	plan.ActionID = types.StringValue(actionID)
	plan.ID = types.StringValue(actionID)
	plan.URN = types.StringValue(urn)
	plan.Category = nullIfEmpty(strVal(plan.Category))
	plan.Description = nullIfEmpty(strVal(plan.Description))
	plan.ExecutorID = nullIfEmpty(strVal(plan.ExecutorID))
	plan.Version = nullIfEmpty(strVal(plan.Version))
}

func (r *actionPipelineResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var state actionPipelineResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	actionID := strings.TrimSpace(state.ActionID.ValueString())
	if actionID == "" {
		actionID = strings.TrimSpace(state.ID.ValueString())
	}
	if actionID == "" {
		resp.Diagnostics.AddError("Invalid state", "Missing action_id/id in state; cannot read remote action pipeline.")
		return
	}

	info, err := r.client.GetActionPipelineByID(ctx, actionID)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if info == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ActionID = types.StringValue(info.ID)
	state.ID = types.StringValue(info.ID)
	state.URN = types.StringValue(datahub.ActionPipelineURNPrefix + info.ID)
	if info.Name != "" {
		state.Name = types.StringValue(info.Name)
	}
	if info.Type != "" {
		state.Type = types.StringValue(info.Type)
	}
	state.Category = nullIfEmpty(info.Category)
	state.Description = nullIfEmpty(info.Description)
	state.ExecutorID = nullIfEmpty(info.ExecutorID)
	state.Version = nullIfEmpty(info.Version)
	if info.Recipe != "" {
		state.Recipe = jsontypes.NewNormalizedValue(info.Recipe)
	}
	if info.DebugMode != nil {
		state.DebugMode = types.BoolValue(*info.DebugMode)
	} else {
		state.DebugMode = types.BoolNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *actionPipelineResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var state actionPipelineResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	actionID := strings.TrimSpace(state.ActionID.ValueString())
	if actionID == "" {
		actionID = strings.TrimSpace(state.ID.ValueString())
	}
	if actionID == "" {
		resp.State.RemoveResource(ctx)
		return
	}
	if err := r.client.DeleteActionPipeline(ctx, actionID); err != nil {
		if errors.Is(err, datahub.ErrActionPipelineCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required", "datahub_action_pipeline requires DataHub Cloud.")
			return
		}
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *actionPipelineResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID",
			"Expected a DataHub action pipeline URN (e.g. urn:li:dataHubAction:my-action) or a bare action_id.")
		return
	}
	actionID := strings.TrimPrefix(raw, datahub.ActionPipelineURNPrefix)
	if actionID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Could not extract an action_id from the provided import ID.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), actionID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("action_id"), actionID)...)
}
