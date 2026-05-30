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

const dataHubRoleURNPrefix = "urn:li:dataHubRole:"

var (
	_ datasource.DataSource              = &roleDataSource{}
	_ datasource.DataSourceWithConfigure = &roleDataSource{}
)

type roleDataSource struct {
	client *datahub.Client
}

type roleDataSourceModel struct {
	Name        types.String `tfsdk:"name"`
	URN         types.String `tfsdk:"urn"`
	Description types.String `tfsdk:"description"`
	Editable    types.Bool   `tfsdk:"editable"`
}

// NewRoleDataSource returns the singular datahub_role lookup data source.
func NewRoleDataSource() datasource.DataSource {
	return &roleDataSource{}
}

func (d *roleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (d *roleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Looks up a built-in DataHub role by name and returns its URN.\n\n" +
			"DataHub ships three built-in roles -- `Admin`, `Editor`, and `Reader` -- seeded at " +
			"bootstrap. They are not creatable, editable, or deletable. Use this data source to " +
			"resolve a role name to its URN for use with `datahub_role_assignment`.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The role name to look up: `Admin`, `Editor`, or `Reader`. Becomes the URN suffix (`urn:li:dataHubRole:<name>`).",
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this role (e.g. `urn:li:dataHubRole:Admin`).",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Human-readable description of the role.",
			},
			"editable": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the role is editable. Built-in roles return `false`.",
			},
		},
	}
}

func (d *roleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *roleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var config roleDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := config.Name.ValueString()
	urn := dataHubRoleURNPrefix + name

	role, err := d.client.GetRoleByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if role == nil {
		resp.Diagnostics.AddError(
			"Role not found",
			fmt.Sprintf("No role named %q was found in DataHub. Valid built-in roles are Admin, Editor, and Reader.", name),
		)
		return
	}

	state := roleDataSourceModel{
		Name:        types.StringValue(role.Name),
		URN:         types.StringValue(role.URN),
		Description: types.StringValue(role.Description),
		Editable:    types.BoolValue(role.Editable),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
