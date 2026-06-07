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
	_ datasource.DataSource              = &ownershipTypesDataSource{}
	_ datasource.DataSourceWithConfigure = &ownershipTypesDataSource{}
)

type ownershipTypesDataSource struct {
	client *datahub.Client
}

type ownershipTypesDataSourceModel struct {
	URNs types.List `tfsdk:"urns"`
}

// NewOwnershipTypesDataSource returns the datahub_ownership_types data source.
func NewOwnershipTypesDataSource() datasource.DataSource {
	return &ownershipTypesDataSource{}
}

func (d *ownershipTypesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ownership_types"
}

func (d *ownershipTypesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Returns the URNs of all DataHub ownership types visible to the authenticated " +
			"principal, including built-in system types (`__system__technical_owner`, etc.).\n\n" +
			"Backed by the `listOwnershipTypes` GraphQL query. Use the returned `urns` list as " +
			"the `for_each` value in `import {}` blocks to bulk-import existing custom ownership " +
			"types into Terraform state.",
		Attributes: map[string]schema.Attribute{
			"urns": schema.ListAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "List of DataHub URNs for all ownership types (e.g. `[\"urn:li:ownershipType:data_quality_lead\"]`).",
			},
		},
	}
}

func (d *ownershipTypesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ownershipTypesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	urns, err := d.client.ListOwnershipTypeURNs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list ownership types", err.Error())
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

	resp.Diagnostics.Append(resp.State.Set(ctx, ownershipTypesDataSourceModel{URNs: urnList})...)
}
