// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ datasource.DataSource              = &ingestionSourceDataSource{}
	_ datasource.DataSourceWithConfigure = &ingestionSourceDataSource{}
)

type ingestionSourceDataSource struct {
	client *datahub.Client
}

type ingestionSourceDataSourceModel struct {
	ID               types.String `tfsdk:"id"`
	SourceID         types.String `tfsdk:"source_id"`
	URN              types.String `tfsdk:"urn"`
	SourceName       types.String `tfsdk:"source_name"`
	SourceType       types.String `tfsdk:"source_type"`
	Platform         types.String `tfsdk:"platform"`
	CronInterval     types.String `tfsdk:"cron_interval"`
	Timezone         types.String `tfsdk:"timezone"`
	CLIVersion       types.String `tfsdk:"cli_version"`
	RemoteExecutorID types.String `tfsdk:"remote_executor_id"`
	ExtraArgs        types.Map    `tfsdk:"extra_args"`
	DebugMode        types.Bool   `tfsdk:"debug_mode"`
	Recipe           types.String `tfsdk:"recipe"`
}

func NewIngestionSourceDataSource() datasource.DataSource {
	return &ingestionSourceDataSource{}
}

func (d *ingestionSourceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ingestion_source"
}

func (d *ingestionSourceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an existing DataHub Ingestion Source by `source_id`.\n\n" +
			"Use this data source to reference an ingestion source that already exists in DataHub " +
			"-- for example, one created via the DataHub UI or a different Terraform root module.\n\n" +
			"## Finding `source_id` for a UI-created source\n\n" +
			"If the ingestion source was created in the DataHub UI, the `source_id` appears in the " +
			"browser URL when you open the source detail page:\n\n" +
			"> `https://<your-datahub>/ingestion/sources/<source_id>`\n\n" +
			"## Recipe\n\n" +
			"The `recipe` attribute is returned as a JSON-encoded string. Use `jsondecode(...)` " +
			"to access individual fields:\n\n" +
			"```terraform\n" +
			"output \"warehouse_host\" {\n" +
			"  value = jsondecode(data.datahub_ingestion_source.warehouse.recipe).source.config.host_port\n" +
			"}\n" +
			"```",
		Attributes: map[string]schema.Attribute{
			"source_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The unique identifier of the ingestion source to look up.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Mirrors `source_id`; provided for framework compatibility.",
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this ingestion source (e.g. `urn:li:dataHubIngestionSource:prod-postgres-abc123`).",
			},
			"source_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Human-readable display name of the ingestion source.",
			},
			"source_type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Ingestion source type (e.g. `postgres`, `bigquery`, `csv-enricher`).",
			},
			"platform": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "DataHub platform URN associated with the source (e.g. `urn:li:dataPlatform:postgres`). Empty if not set.",
			},
			"cron_interval": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Cron expression for the scheduled run interval. Empty if no schedule is configured.",
			},
			"timezone": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Timezone for `cron_interval` (e.g. `America/Los_Angeles`). Empty if no schedule is configured.",
			},
			"cli_version": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Pinned `datahub-ingestion` CLI version. Empty if not pinned.",
			},
			"remote_executor_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Executor or Remote Executor Pool ID that runs this source. Empty if not set.",
			},
			"extra_args": schema.MapAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "Additional key/value arguments passed to the ingestion executor.",
			},
			"debug_mode": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether debug mode is enabled for this ingestion run.",
			},
			"recipe": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Ingestion recipe as a JSON-encoded string. Use `jsondecode(...)` to access specific fields.",
			},
		},
	}
}

func (d *ingestionSourceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ingestionSourceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var config ingestionSourceDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sourceID := config.SourceID.ValueString()
	body, err := d.client.GetIngestionSourceByID(ctx, sourceID)
	if err != nil {
		errLower := strings.ToLower(err.Error())
		if strings.Contains(errLower, "404") || strings.Contains(errLower, "not found") {
			resp.Diagnostics.AddError(
				"Ingestion source not found",
				fmt.Sprintf("No ingestion source with ID %q was found in DataHub. Verify the source_id and retry.", sourceID),
			)
			return
		}
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	var remote datahub.IngestionSource
	if err := json.Unmarshal(body, &remote); err != nil {
		resp.Diagnostics.AddError("Invalid API response", fmt.Sprintf("Failed to parse ingestion source response JSON: %v", err))
		return
	}

	state := ingestionSourceDataSourceModel{
		ID:       types.StringValue(sourceID),
		SourceID: types.StringValue(sourceID),
		URN:      types.StringValue(fmt.Sprintf("urn:li:dataHubIngestionSource:%s", sourceID)),
	}

	if remote.DataHubIngestionSourceInfo.Value.Name != "" {
		state.SourceName = types.StringValue(remote.DataHubIngestionSourceInfo.Value.Name)
	} else {
		state.SourceName = types.StringNull()
	}
	if remote.DataHubIngestionSourceInfo.Value.Type != "" {
		state.SourceType = types.StringValue(remote.DataHubIngestionSourceInfo.Value.Type)
	} else {
		state.SourceType = types.StringNull()
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
		mv, diags := types.MapValue(types.StringType, elems)
		resp.Diagnostics.Append(diags...)
		if !resp.Diagnostics.HasError() {
			state.ExtraArgs = mv
		}
	} else {
		state.ExtraArgs = types.MapNull(types.StringType)
	}
	if remoteRecipe := strings.TrimSpace(remote.DataHubIngestionSourceInfo.Value.Config.Recipe); remoteRecipe != "" {
		state.Recipe = types.StringValue(remoteRecipe)
	} else {
		state.Recipe = types.StringNull()
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

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
