// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ datasource.DataSource              = &connectionsDataSource{}
	_ datasource.DataSourceWithConfigure = &connectionsDataSource{}
)

type connectionsDataSource struct {
	client *datahub.Client
}

type connectionsDataSourceModel struct {
	URNs types.List `tfsdk:"urns"`
}

// NewConnectionsDataSource returns the datahub_connections data source.
func NewConnectionsDataSource() datasource.DataSource {
	return &connectionsDataSource{}
}

func (d *connectionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connections"
}

func (d *connectionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Returns the URNs of all DataHub Connections visible to the authenticated principal.\n\n" +
			"Backed by `searchAcrossEntities` with entity type `DATAHUB_CONNECTION` (OpenSearch). " +
			"Entities created within the last few seconds may not yet appear. Use the returned `urns` " +
			"list as the `for_each` value in `import {}` blocks to bulk-import existing connections " +
			"into Terraform state.\n\n" +
			"Connection credentials are encrypted at rest; only top-level metadata (`name`, `platform`) " +
			"is readable after import. Sensitive fields must be re-supplied in configuration.",
		Attributes: map[string]schema.Attribute{
			"urns": schema.ListAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "List of DataHub URNs for all connections (e.g. `[\"urn:li:dataHubConnection:prod-databricks\"]`).",
			},
		},
	}
}

func (d *connectionsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *connectionsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	urns, err := d.client.ListConnectionURNs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list connections", err.Error())
		return
	}

	urnVals := make([]types.String, len(urns))
	for i, u := range urns {
		urnVals[i] = types.StringValue(u)
	}
	urnList, diags := types.ListValueFrom(ctx, types.StringType, urnVals)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, connectionsDataSourceModel{URNs: urnList})...)
}
