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
	_ datasource.DataSource              = &glossaryTermsDataSource{}
	_ datasource.DataSourceWithConfigure = &glossaryTermsDataSource{}
)

type glossaryTermsDataSource struct {
	client *datahub.Client
}

type glossaryTermsDataSourceModel struct {
	URNs types.List `tfsdk:"urns"`
}

// NewGlossaryTermsDataSource returns the datahub_glossary_terms data source.
func NewGlossaryTermsDataSource() datasource.DataSource {
	return &glossaryTermsDataSource{}
}

func (d *glossaryTermsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_glossary_terms"
}

func (d *glossaryTermsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Returns the URNs of all DataHub glossary terms visible to the authenticated " +
			"principal.\n\n" +
			"Backed by `searchAcrossEntities` (OpenSearch). Terms created within the last " +
			"few seconds may not yet appear. Use the returned `urns` list as the `for_each` " +
			"value in `import {}` blocks to bulk-import an existing glossary into Terraform " +
			"state.",
		Attributes: map[string]schema.Attribute{
			"urns": schema.ListAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "List of DataHub URNs for all glossary terms (e.g. `[\"urn:li:glossaryTerm:revenue\"]`).",
			},
		},
	}
}

func (d *glossaryTermsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *glossaryTermsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	urns, err := d.client.ListGlossaryTermURNs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list glossary terms", err.Error())
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

	resp.Diagnostics.Append(resp.State.Set(ctx, glossaryTermsDataSourceModel{URNs: urnList})...)
}
