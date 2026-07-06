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
	_ datasource.DataSource              = &serviceAccountsDataSource{}
	_ datasource.DataSourceWithConfigure = &serviceAccountsDataSource{}
)

type serviceAccountsDataSource struct {
	client *datahub.Client
}

type serviceAccountsDataSourceModel struct {
	URNs types.List `tfsdk:"urns"`
}

// NewServiceAccountsDataSource returns the datahub_service_accounts data source.
func NewServiceAccountsDataSource() datasource.DataSource {
	return &serviceAccountsDataSource{}
}

func (d *serviceAccountsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_accounts"
}

func (d *serviceAccountsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Returns the URNs of all DataHub service accounts.\n\n" +
			"Requires DataHub Core >= 1.4.0 or DataHub Cloud, and the `Manage Service Accounts` " +
			"privilege. Backed by the `listServiceAccounts` GraphQL query (OpenSearch-backed, " +
			"eventually consistent -- newly created accounts may lag). Use the returned `urns` list " +
			"as the `for_each` value in `import {}` blocks to bulk-import existing service accounts " +
			"into Terraform state; do not rely on it for authoritative reads.",
		Attributes: map[string]schema.Attribute{
			"urns": schema.ListAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "List of DataHub URNs for all service accounts (e.g. `[\"urn:li:corpuser:service_ci-bot\"]`).",
			},
		},
	}
}

func (d *serviceAccountsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *serviceAccountsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	urns, err := d.client.ListServiceAccountURNs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list service accounts", err.Error())
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

	resp.Diagnostics.Append(resp.State.Set(ctx, serviceAccountsDataSourceModel{URNs: urnList})...)
}
