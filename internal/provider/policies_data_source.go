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
	_ datasource.DataSource              = &policiesDataSource{}
	_ datasource.DataSourceWithConfigure = &policiesDataSource{}
)

type policiesDataSource struct {
	client *datahub.Client
}

type policiesDataSourceModel struct {
	URNs types.List `tfsdk:"urns"`
}

// NewPoliciesDataSource returns the datahub_policies data source.
func NewPoliciesDataSource() datasource.DataSource {
	return &policiesDataSource{}
}

func (d *policiesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policies"
}

func (d *policiesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Returns the URNs of all DataHub policies visible to the authenticated principal.\n\n" +
			"Backed by `listPolicies` (OpenSearch). Policies created within the last few seconds may " +
			"not yet appear. Use the returned `urns` list as the `for_each` value in `import {}` " +
			"blocks to bulk-import existing policies into Terraform state.\n\n" +
			"## Import your own policies, not DataHub's\n\n" +
			"This list is everything the caller can see, which includes the default policies DataHub " +
			"itself ships and maintains. Those belong to DataHub, and importing them has a specific " +
			"consequence beyond the usual reasons not to manage a platform's built-ins.\n\n" +
			"Nine of DataHub's sixteen default policies bind their actors entirely through DataHub " +
			"**roles** -- `Admin`, `Editor`, `Reader` -- naming no users and no groups. DataHub's " +
			"policy write API cannot carry a role binding: the `updatePolicy` mutation has no field " +
			"for one, and the server rebuilds the whole policy from that mutation's input. So writing " +
			"such a policy from anywhere -- this provider, the DataHub UI, any other client -- deletes " +
			"its role binding, and a policy that bound its actors only through roles is then granting " +
			"nothing to anybody. Repairing it needs a direct aspect write, since saving it in the UI " +
			"goes through the same mutation. This is a server-side limitation, tracked upstream as " +
			"OSS-1216; no provider version changes it.\n\n" +
			"`datahub_policy` will not make that write -- it refuses at apply time and warns as soon " +
			"as a role-bearing policy is imported -- so the failure mode is a blocked apply rather " +
			"than a broken deployment. The blocked apply is still yours to unpick, so it is worth not " +
			"importing them in the first place.\n\n" +
			"Select the policies you mean to own rather than excluding the ones you do not. An " +
			"allowlist stays correct when a DataHub upgrade adds a default policy; a list of URNs to " +
			"skip does not:\n\n" +
			"```terraform\n" +
			"locals {\n" +
			"  # Only policies this configuration is meant to own, identified by an\n" +
			"  # id prefix the team applies to everything it creates.\n" +
			"  managed_policy_urns = [\n" +
			"    for urn in data.datahub_policies.all.urns :\n" +
			"    urn if startswith(urn, \"urn:li:dataHubPolicy:acme-\")\n" +
			"  ]\n" +
			"}\n" +
			"```\n\n" +
			"For a brownfield estate with no such convention, review the list once by hand: " +
			"DataHub's default policy URNs are fixed, and are listed in `boot/policies.json` in the " +
			"DataHub repository.",
		Attributes: map[string]schema.Attribute{
			"urns": schema.ListAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "List of DataHub URNs for all policies (e.g. `[\"urn:li:dataHubPolicy:my-policy\"]`).",
			},
		},
	}
}

func (d *policiesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *policiesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	urns, err := d.client.ListPolicyURNs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list policies", err.Error())
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

	resp.Diagnostics.Append(resp.State.Set(ctx, policiesDataSourceModel{URNs: urnList})...)
}
