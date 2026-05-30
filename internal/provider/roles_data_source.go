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
	_ datasource.DataSource              = &rolesDataSource{}
	_ datasource.DataSourceWithConfigure = &rolesDataSource{}
)

type rolesDataSource struct {
	client *datahub.Client
}

type rolesDataSourceModel struct {
	URNs types.List `tfsdk:"urns"`
}

// NewRolesDataSource returns the datahub_roles enumeration data source.
func NewRolesDataSource() datasource.DataSource {
	return &rolesDataSource{}
}

func (d *rolesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_roles"
}

func (d *rolesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Returns the URNs of all built-in DataHub roles (`Admin`, `Editor`, `Reader`).",
		Attributes: map[string]schema.Attribute{
			"urns": schema.ListAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "List of DataHub role URNs (e.g. `[\"urn:li:dataHubRole:Admin\", \"urn:li:dataHubRole:Editor\", \"urn:li:dataHubRole:Reader\"]`).",
			},
		},
	}
}

func (d *rolesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *rolesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	urns, err := d.client.ListRoleURNs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list roles", err.Error())
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

	resp.Diagnostics.Append(resp.State.Set(ctx, rolesDataSourceModel{URNs: urnList})...)
}
