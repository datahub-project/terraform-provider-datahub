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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ resource.Resource                = &remoteExecutorPoolResource{}
	_ resource.ResourceWithConfigure   = &remoteExecutorPoolResource{}
	_ resource.ResourceWithImportState = &remoteExecutorPoolResource{}
)

type remoteExecutorPoolResource struct {
	client *datahub.Client
}

type remoteExecutorPoolResourceModel struct {
	ID          types.String `tfsdk:"id"`
	URN         types.String `tfsdk:"urn"`
	PoolID      types.String `tfsdk:"pool_id"`
	Description types.String `tfsdk:"description"`
	IsDefault   types.Bool   `tfsdk:"is_default"`
	IsEmbedded  types.Bool   `tfsdk:"is_embedded"`
	StateStatus types.String `tfsdk:"state_status"`
	StateMsg    types.String `tfsdk:"state_message"`
	CreatedAt   types.Int64  `tfsdk:"created_at"`
}

func NewRemoteExecutorPoolResource() resource.Resource {
	return &remoteExecutorPoolResource{}
}

func (r *remoteExecutorPoolResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *remoteExecutorPoolResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_remote_executor_pool"
}

func (r *remoteExecutorPoolResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Creates and manages a DataHub Remote Executor Pool.\n\n" +
			"**DataHub Cloud only.** Remote Executor Pools are a DataHub Cloud feature. Applying this " +
			"resource against an OSS DataHub instance will fail with a clear error.\n\n" +
			"**API stability notice.** The underlying GraphQL mutations used by this resource are " +
			"classified as `internal` in DataHub Cloud and carry no external API stability guarantee. " +
			"They may change between Cloud releases without notice. This is documented as a known risk " +
			"in the provider; file an issue at https://github.com/datahub-project/terraform-provider-datahub " +
			"if a breaking change is encountered.\n\n" +
			"## What is a Remote Executor Pool?\n\n" +
			"A Remote Executor Pool is a server-side entity that acts as a named registration point " +
			"for one or more Remote Executor worker processes. Workers are deployed in your own " +
			"environment (Kubernetes via the `datahub-executor-worker` Helm chart, or ECS) and connect " +
			"outbound to DataHub Cloud. Each worker references the pool by setting " +
			"`DATAHUB_EXECUTOR_POOL_ID` to the pool's `pool_id`. Workers self-attach when they start " +
			"up; the pool entity itself must already exist.\n\n" +
			"## Reserved pool IDs\n\n" +
			"The pool IDs `default` and `embedded` are reserved by DataHub Cloud and cannot be " +
			"created via this resource. Use the `datahub_remote_executor_pool` data source to " +
			"reference them:\n\n" +
			"```terraform\n" +
			"data \"datahub_remote_executor_pool\" \"default\" {\n" +
			"  pool_id = \"default\"\n" +
			"}\n" +
			"```\n\n" +
			"## Default pool\n\n" +
			"Setting `is_default = true` promotes this pool to the global default for new ingestion " +
			"sources. This atomically demotes any previously-default pool on the server side. " +
			"You cannot directly unset `is_default` to `false` on a default pool; set another pool " +
			"as default instead, and this resource's `is_default` will reflect `false` on the next " +
			"refresh.\n\n" +
			"## Ingestion source linkage\n\n" +
			"To route an ingestion source to this pool, set `remote_executor_id` on the ingestion " +
			"source resource to this pool's `pool_id`:\n\n" +
			"```terraform\n" +
			"resource \"datahub_remote_executor_pool\" \"analytics\" {\n" +
			"  pool_id = \"analytics-team\"\n" +
			"}\n\n" +
			"resource \"datahub_ingestion_source\" \"bigquery\" {\n" +
			"  # ...\n" +
			"  remote_executor_id = datahub_remote_executor_pool.analytics.pool_id\n" +
			"}\n" +
			"```\n\n" +
			"## Pool state\n\n" +
			"After creation, Cloud provisions the underlying infrastructure (e.g. an SQS queue). The " +
			"`state_status` attribute tracks this: `PROVISIONING_PENDING` -> `PROVISIONING_IN_PROGRESS` " +
			"-> `READY`. Workers can connect once the pool reaches `READY`. If provisioning fails, " +
			"`state_status` is `PROVISIONING_FAILED` and `state_message` contains the error detail.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this pool (e.g. `urn:li:dataHubRemoteExecutorPool:my-pool`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"pool_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Unique identifier for the pool. Must contain only alphanumeric characters, " +
					"underscores (`_`), dots (`.`), or hyphens (`-`). The IDs `default` and `embedded` are " +
					"reserved; use the `datahub_remote_executor_pool` data source to reference them. " +
					"Changing this value forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional human-readable description for the pool.",
			},
			"is_default": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Whether this pool is the global default. When set to `true`, this pool " +
					"becomes the default for new ingestion sources, and any previously-default pool is " +
					"automatically demoted. Cannot be directly set to `false` on a default pool; " +
					"set another pool as default instead.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"is_embedded": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether this pool is embedded in the DataHub Cloud coordinator pod. Managed by DataHub Cloud; not configurable.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"state_status": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Provisioning state of the pool: `PROVISIONING_PENDING`, " +
					"`PROVISIONING_IN_PROGRESS`, `READY`, `PROVISIONING_FAILED`, or `OLD_CHANNEL_DRAINING`. " +
					"Workers can connect once the pool reaches `READY`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"state_message": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Error message when `state_status` is `PROVISIONING_FAILED`, otherwise empty.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "UTC timestamp (milliseconds since epoch) when this pool was created.",
			},
		},
	}
}

func (r *remoteExecutorPoolResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan remoteExecutorPoolResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	poolID := plan.PoolID.ValueString()
	if err := datahub.ValidatePoolID(poolID); err != nil {
		resp.Diagnostics.AddError("Invalid pool_id", err.Error())
		return
	}

	isDefault := false
	if !plan.IsDefault.IsNull() && !plan.IsDefault.IsUnknown() {
		isDefault = plan.IsDefault.ValueBool()
	}

	urn, err := r.client.CreateRemoteExecutorPool(ctx, datahub.CreateRemoteExecutorPoolInput{
		PoolID:      poolID,
		Description: plan.Description.ValueString(),
		IsDefault:   isDefault,
	})
	if err != nil {
		if errors.Is(err, datahub.ErrExecutorPoolCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required", err.Error())
		} else {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
		}
		return
	}

	plan.URN = types.StringValue(urn)
	plan.ID = types.StringValue(urn)

	// Wait for the pool to reach READY state before returning. SQS-channel pools
	// are provisioned asynchronously (PROVISIONING_PENDING -> READY); KAFKA-channel
	// pools start READY immediately so the first poll returns without sleeping.
	pool, err := r.client.WaitForRemoteExecutorPoolReady(ctx, urn, 0)
	if err != nil {
		if errors.Is(err, datahub.ErrExecutorPoolCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required", err.Error())
		} else {
			resp.Diagnostics.AddError("DataHub API Error", "Pool was created but did not reach READY state: "+err.Error())
		}
		return
	}
	if pool != nil {
		populatePoolModel(&plan, pool)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *remoteExecutorPoolResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state remoteExecutorPoolResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}
	if urn == "" {
		urn = fmt.Sprintf("urn:li:dataHubRemoteExecutorPool:%s", state.PoolID.ValueString())
	}

	pool, err := r.client.GetRemoteExecutorPoolByURN(ctx, urn)
	if err != nil {
		if errors.Is(err, datahub.ErrExecutorPoolCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required", err.Error())
		} else {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
		}
		return
	}
	if pool == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.URN = types.StringValue(pool.URN)
	state.ID = types.StringValue(pool.URN)
	state.PoolID = types.StringValue(pool.PoolID)
	populatePoolModel(&state, pool)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *remoteExecutorPoolResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan remoteExecutorPoolResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state remoteExecutorPoolResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}

	// Guard: cannot directly demote a default pool; requires promoting another.
	planDefault := !plan.IsDefault.IsNull() && !plan.IsDefault.IsUnknown() && plan.IsDefault.ValueBool()
	stateDefault := !state.IsDefault.IsNull() && !state.IsDefault.IsUnknown() && state.IsDefault.ValueBool()
	if stateDefault && !planDefault {
		resp.Diagnostics.AddError(
			"Cannot unset default pool",
			"datahub_remote_executor_pool: to change the default pool, set is_default = true on another pool. "+
				"Setting is_default = false on a default pool directly is not supported.",
		)
		return
	}

	// Update description if it changed.
	if !plan.Description.Equal(state.Description) {
		desc := plan.Description.ValueString()
		if err := r.client.UpdateRemoteExecutorPool(ctx, datahub.UpdateRemoteExecutorPoolInput{
			URN:         urn,
			Description: &desc,
		}); err != nil {
			if errors.Is(err, datahub.ErrExecutorPoolCloudOnly) {
				resp.Diagnostics.AddError("DataHub Cloud Required", err.Error())
			} else {
				resp.Diagnostics.AddError("DataHub API Error", err.Error())
			}
			return
		}
	}

	// Promote to default if requested.
	if planDefault && !stateDefault {
		if err := r.client.SetDefaultRemoteExecutorPool(ctx, urn); err != nil {
			if errors.Is(err, datahub.ErrExecutorPoolCloudOnly) {
				resp.Diagnostics.AddError("DataHub Cloud Required", err.Error())
			} else {
				resp.Diagnostics.AddError("DataHub API Error", err.Error())
			}
			return
		}
	}

	// Read back to populate all computed fields.
	pool, err := r.client.GetRemoteExecutorPoolByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.URN = state.URN
	plan.ID = state.ID
	if pool != nil {
		populatePoolModel(&plan, pool)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *remoteExecutorPoolResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state remoteExecutorPoolResourceModel
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

	// Guard: never delete an embedded pool (the built-in coordinator-pod executor).
	// Deleting it would break routing for the embedded executor and cannot be undone
	// without a DataHub Cloud support ticket.
	if !state.IsEmbedded.IsNull() && !state.IsEmbedded.IsUnknown() && state.IsEmbedded.ValueBool() {
		resp.Diagnostics.AddError(
			"Cannot delete embedded pool",
			"Pool "+state.PoolID.ValueString()+" is the DataHub Cloud embedded executor pool and cannot be "+
				"deleted via Terraform. Remove this resource block from your configuration to stop managing it.",
		)
		return
	}

	// Warn if deleting the current default pool: Cloud does not clear the global
	// default pointer on delete, leaving it stale until another pool is promoted.
	// Set is_default = true on another pool resource before destroying this one.
	if !state.IsDefault.IsNull() && !state.IsDefault.IsUnknown() && state.IsDefault.ValueBool() {
		resp.Diagnostics.AddWarning(
			"Deleting the default executor pool",
			"Pool "+state.PoolID.ValueString()+" is currently the default executor pool. DataHub Cloud does not "+
				"automatically clear the global default pointer when a pool is deleted. Set is_default = true "+
				"on another pool resource before destroying this one to avoid a stale default pointer.",
		)
	}

	if err := r.client.DeleteRemoteExecutorPool(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *remoteExecutorPoolResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a pool URN (e.g. urn:li:dataHubRemoteExecutorPool:my-pool) or a bare pool ID.")
		return
	}

	const urnPrefix = "urn:li:dataHubRemoteExecutorPool:"
	var poolID, urn string
	if strings.HasPrefix(raw, urnPrefix) {
		urn = raw
		poolID = strings.TrimPrefix(raw, urnPrefix)
	} else {
		poolID = raw
		urn = urnPrefix + poolID
	}

	pool, err := r.client.GetRemoteExecutorPoolByURN(ctx, urn)
	if err != nil {
		if errors.Is(err, datahub.ErrExecutorPoolCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required", err.Error())
		} else {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
		}
		return
	}
	if pool == nil {
		resp.Diagnostics.AddError(
			"Pool not found",
			fmt.Sprintf("No remote executor pool with ID %q was found in DataHub. Verify the pool ID or URN and retry.", poolID),
		)
		return
	}

	state := remoteExecutorPoolResourceModel{
		ID:     types.StringValue(pool.URN),
		URN:    types.StringValue(pool.URN),
		PoolID: types.StringValue(pool.PoolID),
	}
	populatePoolModel(&state, pool)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// populatePoolModel copies read-back fields from a RemoteExecutorPool into the model.
func populatePoolModel(m *remoteExecutorPoolResourceModel, p *datahub.RemoteExecutorPool) {
	if p.Description != "" {
		m.Description = types.StringValue(p.Description)
	} else {
		m.Description = types.StringNull()
	}
	m.IsDefault = types.BoolValue(p.IsDefault)
	m.IsEmbedded = types.BoolValue(p.IsEmbedded)
	m.CreatedAt = types.Int64Value(p.CreatedAt)

	if p.StateStatus != "" {
		m.StateStatus = types.StringValue(p.StateStatus)
	} else {
		m.StateStatus = types.StringNull()
	}
	if p.StateMsg != "" {
		m.StateMsg = types.StringValue(p.StateMsg)
	} else {
		m.StateMsg = types.StringNull()
	}
}
