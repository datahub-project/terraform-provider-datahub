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

const corpUserURNPrefix = "urn:li:corpuser:"

var (
	_ datasource.DataSource              = &corpUserDataSource{}
	_ datasource.DataSourceWithConfigure = &corpUserDataSource{}
)

type corpUserDataSource struct {
	client *datahub.Client
}

type corpUserDataSourceModel struct {
	Username    types.String `tfsdk:"username"`
	URN         types.String `tfsdk:"urn"`
	DisplayName types.String `tfsdk:"display_name"`
	Email       types.String `tfsdk:"email"`
	Title       types.String `tfsdk:"title"`
	Active      types.Bool   `tfsdk:"active"`
	Status      types.String `tfsdk:"status"`
}

// NewCorpUserDataSource returns the datahub_corp_user lookup data source.
func NewCorpUserDataSource() datasource.DataSource {
	return &corpUserDataSource{}
}

func (d *corpUserDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_corp_user"
}

func (d *corpUserDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Looks up an existing DataHub user (`corpUser`) by `username`.\n\n" +
			"Use this to resolve a username to its URN for use as a policy actor, group member, " +
			"or ownership reference -- for example `data.datahub_corp_user.alice.urn`.\n\n" +
			"This provider does not create users (there is no clean API to provision a login-capable " +
			"user with a password). Users are typically created via SSO/JIT provisioning or the " +
			"DataHub invite flow; this data source reads their catalog record once they exist.",
		Attributes: map[string]schema.Attribute{
			"username": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The username of the user to look up. Becomes the URN suffix (`urn:li:corpuser:<username>`).",
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this user (e.g. `urn:li:corpuser:alice`).",
			},
			"display_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Display name of the user. Empty if not set.",
			},
			"email": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Email address of the user. Empty if not set.",
			},
			"title": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Job title of the user. Empty if not set.",
			},
			"active": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the user account is active.",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Provisioning status of the user (e.g. `ACTIVE`). Empty if the status aspect is not set.",
			},
		},
	}
}

func (d *corpUserDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *corpUserDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var config corpUserDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	username := config.Username.ValueString()
	urn := corpUserURNPrefix + username

	user, err := d.client.GetUserByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if user == nil {
		resp.Diagnostics.AddError(
			"User not found",
			fmt.Sprintf("No user with username %q was found in DataHub. Verify the username and retry.", username),
		)
		return
	}

	state := corpUserDataSourceModel{
		Username:    types.StringValue(user.Username),
		URN:         types.StringValue(user.URN),
		DisplayName: types.StringValue(user.DisplayName),
		Email:       types.StringValue(user.Email),
		Title:       types.StringValue(user.Title),
		Active:      types.BoolValue(user.Active),
		Status:      types.StringValue(user.Status),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
