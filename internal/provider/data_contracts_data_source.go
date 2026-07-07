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
	_ datasource.DataSource              = &dataContractsDataSource{}
	_ datasource.DataSourceWithConfigure = &dataContractsDataSource{}
)

type dataContractsDataSource struct {
	client *datahub.Client
}

type dataContractsDataSourceModel struct {
	URNs types.List `tfsdk:"urns"`
}

// NewDataContractsDataSource returns the datahub_data_contracts data source.
func NewDataContractsDataSource() datasource.DataSource {
	return &dataContractsDataSource{}
}

func (d *dataContractsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_contracts"
}

func (d *dataContractsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Returns the URNs of all DataHub data contracts visible to the authenticated principal.\n\n" +
			"Backed by `searchAcrossEntities` (OpenSearch). Contracts created within the last few seconds " +
			"may not yet appear. Feed the `urns` output into an `import {}` for-each block to bulk-import " +
			"existing contracts.",
		Attributes: map[string]schema.Attribute{
			"urns": schema.ListAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "List of data contract URNs (e.g. `[\"urn:li:dataContract:<id>\"]`).",
			},
		},
	}
}

func (d *dataContractsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *dataContractsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	urns, err := d.client.ListDataContractURNs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list data contracts", err.Error())
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

	resp.Diagnostics.Append(resp.State.Set(ctx, dataContractsDataSourceModel{URNs: urnList})...)
}
