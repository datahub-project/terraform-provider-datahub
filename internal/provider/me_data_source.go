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
	_ datasource.DataSource              = &meDataSource{}
	_ datasource.DataSourceWithConfigure = &meDataSource{}
)

type meDataSource struct {
	client *datahub.Client
}

type meDataSourceModel struct {
	Urn         types.String `tfsdk:"urn"`
	Username    types.String `tfsdk:"username"`
	Type        types.String `tfsdk:"type"`
	DisplayName types.String `tfsdk:"display_name"`
	Email       types.String `tfsdk:"email"`
}

func NewMeDataSource() datasource.DataSource {
	return &meDataSource{}
}

func (d *meDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_me"
}

func (d *meDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Returns the identity of the authenticated DataHub user.\n\n" +
			"Reading this data source makes a lightweight GraphQL call to DataHub. Referencing it in " +
			"your configuration is a convenient way to smoke-test provider credentials at `terraform plan` " +
			"time without creating any resources.",
		Attributes: map[string]schema.Attribute{
			"urn": schema.StringAttribute{
				MarkdownDescription: "The DataHub URN of the authenticated user (e.g. `urn:li:corpuser:jane`).",
				Computed:            true,
			},
			"username": schema.StringAttribute{
				MarkdownDescription: "The DataHub username of the authenticated user.",
				Computed:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "The DataHub entity type of the authenticated user (typically `CORP_USER`).",
				Computed:            true,
			},
			"display_name": schema.StringAttribute{
				MarkdownDescription: "The display name of the authenticated user, if set.",
				Computed:            true,
			},
			"email": schema.StringAttribute{
				MarkdownDescription: "The email address of the authenticated user, if set.",
				Computed:            true,
			},
		},
	}
}

func (d *meDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *meDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	identity, err := d.client.Me(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read authenticated identity",
			"The datahub_me data source could not retrieve the authenticated user: "+err.Error(),
		)
		return
	}

	state := meDataSourceModel{
		Urn:         types.StringValue(identity.Urn),
		Username:    types.StringValue(identity.Username),
		Type:        types.StringValue(identity.Type),
		DisplayName: types.StringValue(identity.DisplayName),
		Email:       types.StringValue(identity.Email),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
