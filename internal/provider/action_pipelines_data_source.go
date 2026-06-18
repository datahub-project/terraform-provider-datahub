// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ datasource.DataSource              = &actionPipelinesDataSource{}
	_ datasource.DataSourceWithConfigure = &actionPipelinesDataSource{}
)

type actionPipelinesDataSource struct {
	client *datahub.Client
}

type actionPipelinesDataSourceModel struct {
	URNs types.List `tfsdk:"urns"`
}

// NewActionPipelinesDataSource returns the datahub_action_pipelines data source.
func NewActionPipelinesDataSource() datasource.DataSource {
	return &actionPipelinesDataSource{}
}

func (d *actionPipelinesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_action_pipelines"
}

func (d *actionPipelinesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: cloudOnlyBadge +
			"Returns the URNs of all DataHub Cloud action pipelines visible to the authenticated " +
			"principal, for bulk import via `for_each` into `import {}` blocks.\n\n" +
			"Backed by the `listActionPipelines` GraphQL query (eventually consistent; a pipeline " +
			"created within the last few seconds may not yet appear). Requires DataHub Cloud.",
		Attributes: map[string]schema.Attribute{
			"urns": schema.ListAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "List of DataHub action pipeline URNs (e.g. `[\"urn:li:dataHubAction:<id>\"]`).",
			},
		},
	}
}

func (d *actionPipelinesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *actionPipelinesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	urns, err := d.client.ListActionPipelineURNs(ctx)
	if err != nil {
		if errors.Is(err, datahub.ErrActionPipelineCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required",
				"datahub_action_pipelines requires DataHub Cloud. "+
					"The configured DataHub instance does not expose action pipeline management.")
			return
		}
		resp.Diagnostics.AddError("Failed to list action pipelines", err.Error())
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
	resp.Diagnostics.Append(resp.State.Set(ctx, actionPipelinesDataSourceModel{URNs: urnList})...)
}
