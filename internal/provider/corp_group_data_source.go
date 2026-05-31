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
	_ datasource.DataSource              = &corpGroupDataSource{}
	_ datasource.DataSourceWithConfigure = &corpGroupDataSource{}
)

type corpGroupDataSource struct {
	client *datahub.Client
}

type corpGroupDataSourceModel struct {
	GroupID     types.String `tfsdk:"group_id"`
	URN         types.String `tfsdk:"urn"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Email       types.String `tfsdk:"email"`
	Slack       types.String `tfsdk:"slack"`
}

// NewCorpGroupDataSource returns the singular datahub_corp_group lookup data source.
func NewCorpGroupDataSource() datasource.DataSource {
	return &corpGroupDataSource{}
}

func (d *corpGroupDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_corp_group"
}

func (d *corpGroupDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Looks up an existing DataHub group (`corpGroup`) by `group_id`.\n\n" +
			"Use this to reference a group that already exists in DataHub -- for example one created " +
			"in the DataHub UI or by your identity provider -- as a policy actor or owner.",
		Attributes: map[string]schema.Attribute{
			"group_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The unique identifier of the group to look up (the URN suffix).",
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this group (e.g. `urn:li:corpGroup:data-platform`).",
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Human-readable display name of the group.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description of the group. Empty if not set.",
			},
			"email": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Contact email address for the group. Empty if not set.",
			},
			"slack": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Slack channel or handle for the group. Empty if not set.",
			},
		},
	}
}

func (d *corpGroupDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *corpGroupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var config corpGroupDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groupID := config.GroupID.ValueString()
	urn := corpGroupURNPrefix + groupID

	group, err := d.client.GetGroupByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if group == nil {
		resp.Diagnostics.AddError(
			"Group not found",
			fmt.Sprintf("No group with ID %q was found in DataHub. Verify the group_id and retry.", groupID),
		)
		return
	}

	state := corpGroupDataSourceModel{
		GroupID:     types.StringValue(group.ID),
		URN:         types.StringValue(group.URN),
		Name:        types.StringValue(group.Name),
		Description: types.StringValue(group.Description),
		Email:       types.StringValue(group.Email),
		Slack:       types.StringValue(group.Slack),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
