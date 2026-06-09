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
	_ datasource.DataSource              = &assertionDataSource{}
	_ datasource.DataSourceWithConfigure = &assertionDataSource{}
)

type assertionDataSource struct {
	client *datahub.Client
}

type assertionDataSourceModel struct {
	URN           types.String `tfsdk:"urn"`
	AssertionType types.String `tfsdk:"assertion_type"`
	EntityURN     types.String `tfsdk:"entity_urn"`
}

// NewAssertionDataSource returns the singular datahub_assertion lookup data source.
func NewAssertionDataSource() datasource.DataSource {
	return &assertionDataSource{}
}

func (d *assertionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_assertion"
}

func (d *assertionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Looks up an existing DataHub assertion by URN.\n\n" +
			"Use this to reference an assertion created outside Terraform -- for example via " +
			"the DataHub UI or the `upsertCustomAssertion` API -- without taking ownership " +
			"of it.",
		Attributes: map[string]schema.Attribute{
			"urn": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Full assertion URN to look up (e.g. `urn:li:assertion:<uuid>`).",
			},
			"assertion_type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The assertion type as reported by DataHub (e.g. `FRESHNESS`, `VOLUME`, `SQL`, `CUSTOM`).",
			},
			"entity_urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "URN of the DataHub dataset this assertion monitors.",
			},
		},
	}
}

func (d *assertionDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *assertionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var config assertionDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := config.URN.ValueString()
	ai, err := d.client.GetAssertionByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if ai == nil {
		resp.Diagnostics.AddError(
			"Assertion not found",
			fmt.Sprintf("No assertion with URN %q was found in DataHub. Verify the URN and retry.", urn),
		)
		return
	}

	state := assertionDataSourceModel{
		URN:           types.StringValue(ai.URN),
		AssertionType: types.StringValue(ai.Type),
		EntityURN:     types.StringValue(ai.EntityURN),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
