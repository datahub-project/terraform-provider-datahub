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
	_ datasource.DataSource              = &ingestionSourcesDataSource{}
	_ datasource.DataSourceWithConfigure = &ingestionSourcesDataSource{}
)

type ingestionSourcesDataSource struct {
	client *datahub.Client
}

type ingestionSourcesDataSourceModel struct {
	URNs types.List `tfsdk:"urns"`
}

// NewIngestionSourcesDataSource returns the datahub_ingestion_sources data source.
func NewIngestionSourcesDataSource() datasource.DataSource {
	return &ingestionSourcesDataSource{}
}

func (d *ingestionSourcesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ingestion_sources"
}

func (d *ingestionSourcesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Returns the URNs of all DataHub Ingestion Sources visible to the authenticated principal.\n\n" +
			"Backed by the `listIngestionSources` GraphQL query (OpenSearch). Entities created within " +
			"the last few seconds may not yet appear. Use the returned `urns` list as the `for_each` " +
			"value in `import {}` blocks to bulk-import existing ingestion sources into Terraform state.",
		Attributes: map[string]schema.Attribute{
			"urns": schema.ListAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "List of DataHub URNs for all ingestion sources (e.g. `[\"urn:li:dataHubIngestionSource:prod-postgres\"]`).",
			},
		},
	}
}

func (d *ingestionSourcesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ingestionSourcesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	urns, err := d.client.ListIngestionSourceURNs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list ingestion sources", err.Error())
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

	resp.Diagnostics.Append(resp.State.Set(ctx, ingestionSourcesDataSourceModel{URNs: urnList})...)
}
