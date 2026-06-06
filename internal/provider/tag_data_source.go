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
	_ datasource.DataSource              = &tagDataSource{}
	_ datasource.DataSourceWithConfigure = &tagDataSource{}
)

type tagDataSource struct {
	client *datahub.Client
}

type tagDataSourceModel struct {
	TagID       types.String `tfsdk:"tag_id"`
	URN         types.String `tfsdk:"urn"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	ColorHex    types.String `tfsdk:"color_hex"`
}

// NewTagDataSource returns the singular datahub_tag lookup data source.
func NewTagDataSource() datasource.DataSource {
	return &tagDataSource{}
}

func (d *tagDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tag"
}

func (d *tagDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Looks up an existing DataHub tag by `tag_id`.\n\n" +
			"Use this to reference a tag that already exists in DataHub -- for example one " +
			"created in the DataHub UI or via the Python SDK -- without taking ownership of it.",
		Attributes: map[string]schema.Attribute{
			"tag_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The unique identifier of the tag to look up (the URN suffix, e.g. `pii`).",
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this tag (e.g. `urn:li:tag:pii`).",
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Human-readable display name of the tag.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description of the tag. Empty if not set.",
			},
			"color_hex": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Display colour of the tag badge (e.g. `#FF6B6B`). Empty if not set.",
			},
		},
	}
}

func (d *tagDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *tagDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var config tagDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tagID := config.TagID.ValueString()
	urn := tagURNPrefix + tagID

	tag, err := d.client.GetTagByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if tag == nil {
		resp.Diagnostics.AddError(
			"Tag not found",
			fmt.Sprintf("No tag with ID %q was found in DataHub. Verify the tag_id and retry.", tagID),
		)
		return
	}

	state := tagDataSourceModel{
		TagID:       types.StringValue(tag.ID),
		URN:         types.StringValue(tag.URN),
		Name:        types.StringValue(tag.Name),
		Description: types.StringValue(tag.Description),
		ColorHex:    types.StringValue(tag.ColorHex),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
