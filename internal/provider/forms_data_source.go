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
	_ datasource.DataSource              = &formsDataSource{}
	_ datasource.DataSourceWithConfigure = &formsDataSource{}
)

type formsDataSource struct {
	client *datahub.Client
}

type formsDataSourceModel struct {
	URNs types.List `tfsdk:"urns"`
}

// NewFormsDataSource returns the datahub_forms data source.
func NewFormsDataSource() datasource.DataSource {
	return &formsDataSource{}
}

func (d *formsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_forms"
}

func (d *formsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Returns the URNs of all DataHub forms visible to the authenticated principal.\n\n" +
			"Backed by `searchAcrossEntities` (OpenSearch). Forms created within the last few seconds " +
			"may not yet appear. Feed the `urns` output into an `import {}` for-each block to bulk-import " +
			"existing forms.",
		Attributes: map[string]schema.Attribute{
			"urns": schema.ListAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "List of form URNs (e.g. `[\"urn:li:form:<id>\"]`).",
			},
		},
	}
}

func (d *formsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *formsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	urns, err := d.client.ListFormURNs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list forms", err.Error())
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

	resp.Diagnostics.Append(resp.State.Set(ctx, formsDataSourceModel{URNs: urnList})...)
}
