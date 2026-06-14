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
	_ datasource.DataSource              = &assertionsDataSource{}
	_ datasource.DataSourceWithConfigure = &assertionsDataSource{}
)

type assertionsDataSource struct {
	client *datahub.Client
}

type assertionsDataSourceModel struct {
	URNs types.List `tfsdk:"urns"`
}

// NewAssertionsDataSource returns the datahub_assertions data source.
func NewAssertionsDataSource() datasource.DataSource {
	return &assertionsDataSource{}
}

func (d *assertionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_assertions"
}

func (d *assertionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Returns the URNs of all DataHub assertions visible to the authenticated principal.\n\n" +
			"Backed by `searchAcrossEntities` (OpenSearch). Assertions created within the last few " +
			"seconds may not yet appear.\n\n" +
			"This lists assertions of every type and source (including ingested `EXTERNAL` " +
			"assertions such as dbt tests, and `INFERRED` smart/AI assertions). Not all are " +
			"importable: only NATIVE assertions of a type the provider models import cleanly, and " +
			"the assertion resources refuse a non-NATIVE import. For bulk import prefer " +
			"`datahub-tf-extract enumerate`, which filters to importable assertions automatically; " +
			"use this data source for inventory or when you want to select URNs yourself.",
		Attributes: map[string]schema.Attribute{
			"urns": schema.ListAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "List of DataHub URNs for all assertions (e.g. `[\"urn:li:assertion:<uuid>\"]`).",
			},
		},
	}
}

func (d *assertionsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *assertionsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	urns, err := d.client.ListAssertionURNs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list assertions", err.Error())
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

	resp.Diagnostics.Append(resp.State.Set(ctx, assertionsDataSourceModel{URNs: urnList})...)
}
