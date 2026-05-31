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
	_ datasource.DataSource              = &corpGroupsDataSource{}
	_ datasource.DataSourceWithConfigure = &corpGroupsDataSource{}
)

type corpGroupsDataSource struct {
	client *datahub.Client
}

type corpGroupsDataSourceModel struct {
	URNs types.List `tfsdk:"urns"`
}

// NewCorpGroupsDataSource returns the datahub_corp_groups data source.
func NewCorpGroupsDataSource() datasource.DataSource {
	return &corpGroupsDataSource{}
}

func (d *corpGroupsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_corp_groups"
}

func (d *corpGroupsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Returns the URNs of all DataHub groups visible to the authenticated principal.\n\n" +
			"Backed by `listGroups` (OpenSearch). Groups created within the last few seconds may " +
			"not yet appear. Use the returned `urns` list as the `for_each` value in `import {}` " +
			"blocks to bulk-import existing groups into Terraform state.",
		Attributes: map[string]schema.Attribute{
			"urns": schema.ListAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "List of DataHub URNs for all groups (e.g. `[\"urn:li:corpGroup:data-platform\"]`).",
			},
		},
	}
}

func (d *corpGroupsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *corpGroupsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	urns, err := d.client.ListGroupURNs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list groups", err.Error())
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

	resp.Diagnostics.Append(resp.State.Set(ctx, corpGroupsDataSourceModel{URNs: urnList})...)
}
