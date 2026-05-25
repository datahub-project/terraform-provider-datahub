// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ datasource.DataSource              = &remoteExecutorPoolDataSource{}
	_ datasource.DataSourceWithConfigure = &remoteExecutorPoolDataSource{}
)

type remoteExecutorPoolDataSource struct {
	client *datahub.Client
}

type remoteExecutorPoolDataSourceModel struct {
	PoolID      types.String `tfsdk:"pool_id"`
	URN         types.String `tfsdk:"urn"`
	Description types.String `tfsdk:"description"`
	IsDefault   types.Bool   `tfsdk:"is_default"`
	IsEmbedded  types.Bool   `tfsdk:"is_embedded"`
	StateStatus types.String `tfsdk:"state_status"`
	StateMsg    types.String `tfsdk:"state_message"`
	CreatedAt   types.Int64  `tfsdk:"created_at"`
}

func NewRemoteExecutorPoolDataSource() datasource.DataSource {
	return &remoteExecutorPoolDataSource{}
}

func (d *remoteExecutorPoolDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_remote_executor_pool"
}

func (d *remoteExecutorPoolDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: cloudOnlyBadge +
			"Looks up an existing DataHub Remote Executor Pool by pool ID.\n\n" +
			"## Common use cases\n\n" +
			"**Reference the reserved `default` pool** (auto-provisioned by DataHub Cloud; cannot be " +
			"created via the `datahub_remote_executor_pool` resource):\n\n" +
			"```terraform\n" +
			"data \"datahub_remote_executor_pool\" \"default\" {\n" +
			"  pool_id = \"default\"\n" +
			"}\n\n" +
			"resource \"datahub_ingestion_source\" \"postgres_warehouse\" {\n" +
			"  # ...\n" +
			"  remote_executor_id = data.datahub_remote_executor_pool.default.pool_id\n" +
			"}\n" +
			"```\n\n" +
			"**Reference a pool created by another Terraform root module:**\n\n" +
			"```terraform\n" +
			"data \"datahub_remote_executor_pool\" \"analytics\" {\n" +
			"  pool_id = \"analytics-team\"\n" +
			"}\n" +
			"```",
		Attributes: map[string]schema.Attribute{
			"pool_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The unique pool identifier to look up (e.g. `default`, `analytics-team`).",
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this pool (e.g. `urn:li:dataHubRemoteExecutorPool:default`).",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Human-readable description of the pool.",
			},
			"is_default": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether this pool is the global default for new ingestion sources.",
			},
			"is_embedded": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether this pool is embedded in the DataHub Cloud coordinator pod.",
			},
			"state_status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Provisioning state of the pool: `PROVISIONING_PENDING`, `PROVISIONING_IN_PROGRESS`, `READY`, `PROVISIONING_FAILED`, or `OLD_CHANNEL_DRAINING`.",
			},
			"state_message": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Error message when `state_status` is `PROVISIONING_FAILED`, otherwise empty.",
			},
			"created_at": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "UTC timestamp (milliseconds since epoch) when this pool was created.",
			},
		},
	}
}

func (d *remoteExecutorPoolDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*datahub.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *datahub.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	d.client = client
}

func (d *remoteExecutorPoolDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var config remoteExecutorPoolDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	poolID := config.PoolID.ValueString()
	pool, err := d.client.GetRemoteExecutorPoolByID(ctx, poolID)
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
			fmt.Sprintf("No remote executor pool with ID %q was found in DataHub. Verify the pool ID and retry.", poolID),
		)
		return
	}

	state := remoteExecutorPoolDataSourceModel{
		PoolID:     types.StringValue(pool.PoolID),
		URN:        types.StringValue(pool.URN),
		IsDefault:  types.BoolValue(pool.IsDefault),
		IsEmbedded: types.BoolValue(pool.IsEmbedded),
		CreatedAt:  types.Int64Value(pool.CreatedAt),
	}

	if pool.Description != "" {
		state.Description = types.StringValue(pool.Description)
	} else {
		state.Description = types.StringNull()
	}
	if pool.StateStatus != "" {
		state.StateStatus = types.StringValue(pool.StateStatus)
	} else {
		state.StateStatus = types.StringNull()
	}
	if pool.StateMsg != "" {
		state.StateMsg = types.StringValue(pool.StateMsg)
	} else {
		state.StateMsg = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
