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
	_ datasource.DataSource              = &metadataTestsDataSource{}
	_ datasource.DataSourceWithConfigure = &metadataTestsDataSource{}
)

type metadataTestsDataSource struct {
	client *datahub.Client
}

type metadataTestsDataSourceModel struct {
	URNs types.List `tfsdk:"urns"`
}

// NewMetadataTestsDataSource returns the datahub_metadata_tests data source.
func NewMetadataTestsDataSource() datasource.DataSource {
	return &metadataTestsDataSource{}
}

func (d *metadataTestsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_metadata_tests"
}

func (d *metadataTestsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Returns the URNs of all DataHub metadata tests visible to the authenticated principal, " +
			"for bulk import via `for_each` into `import {}` blocks.\n\n" +
			"Backed by the `listTests` GraphQL query (eventually consistent; a test created within " +
			"the last few seconds may not yet appear).",
		Attributes: map[string]schema.Attribute{
			"urns": schema.ListAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "List of DataHub metadata test URNs (e.g. `[\"urn:li:test:<id>\"]`).",
			},
		},
	}
}

func (d *metadataTestsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *metadataTestsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	urns, err := d.client.ListMetadataTestURNs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list metadata tests", err.Error())
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
	resp.Diagnostics.Append(resp.State.Set(ctx, metadataTestsDataSourceModel{URNs: urnList})...)
}
