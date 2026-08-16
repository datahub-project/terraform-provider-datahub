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
		MarkdownDescription: cloudOnlyBadge +
			"Returns the URNs of all DataHub data contracts visible to the authenticated principal.\n\n" +
			"**Requires DataHub Cloud, unlike the `datahub_data_contract` resource, which works on both.** " +
			"This data source is backed by `searchAcrossEntities`, and the GraphQL type that turns a search " +
			"hit into an entity is registered only on Cloud. The contract is indexed on open-source DataHub " +
			"too, so the search finds it and then has nothing to build it with, and the query fails with a " +
			"non-null violation naming `searchResults[0].entity` rather than returning an empty list. " +
			"Upstream intends to move the type to open source; until then there is no workaround here, and " +
			"the resource's own read path is unaffected because it uses the OpenAPI v3 entity endpoint.\n\n" +
			"Contracts created within the last few seconds may not yet appear, since the backing index is " +
			"eventually consistent. Feed the `urns` output into an `import {}` for-each block to bulk-import " +
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
