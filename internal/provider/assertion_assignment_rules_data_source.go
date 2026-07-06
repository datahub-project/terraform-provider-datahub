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
	_ datasource.DataSource              = &assertionAssignmentRulesDataSource{}
	_ datasource.DataSourceWithConfigure = &assertionAssignmentRulesDataSource{}
)

type assertionAssignmentRulesDataSource struct {
	client *datahub.Client
}

type assertionAssignmentRulesDataSourceModel struct {
	URNs types.List `tfsdk:"urns"`
}

// NewAssertionAssignmentRulesDataSource returns the datahub_assertion_assignment_rules data source.
func NewAssertionAssignmentRulesDataSource() datasource.DataSource {
	return &assertionAssignmentRulesDataSource{}
}

func (d *assertionAssignmentRulesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_assertion_assignment_rules"
}

func (d *assertionAssignmentRulesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: cloudOnlyBadge +
			"Returns the URNs of all DataHub Cloud assertion assignment rules visible to the " +
			"authenticated principal.\n\n" +
			"Backed by `listAssertionAssignmentRules` (OpenSearch). Rules created within the last few " +
			"seconds may not yet appear. Requires DataHub Cloud; errors on OSS DataHub.\n\n" +
			"Feed the `urns` output into an `import {}` for-each block to bulk-import existing rules.",
		Attributes: map[string]schema.Attribute{
			"urns": schema.ListAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "List of assignment rule URNs (e.g. `[\"urn:li:assertionAssignmentRule:<id>\"]`).",
			},
		},
	}
}

func (d *assertionAssignmentRulesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *assertionAssignmentRulesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	urns, err := d.client.ListAssertionAssignmentRuleURNs(ctx)
	if err != nil {
		if errors.Is(err, datahub.ErrAssertionAssignmentRuleCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required",
				"datahub_assertion_assignment_rules requires DataHub Cloud. "+
					"The configured DataHub instance does not expose assertion assignment rule management.")
			return
		}
		resp.Diagnostics.AddError("Failed to list assertion assignment rules", err.Error())
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

	resp.Diagnostics.Append(resp.State.Set(ctx, assertionAssignmentRulesDataSourceModel{URNs: urnList})...)
}
