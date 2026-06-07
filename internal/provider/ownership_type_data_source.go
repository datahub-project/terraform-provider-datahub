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
	_ datasource.DataSource              = &ownershipTypeDataSource{}
	_ datasource.DataSourceWithConfigure = &ownershipTypeDataSource{}
)

type ownershipTypeDataSource struct {
	client *datahub.Client
}

type ownershipTypeDataSourceModel struct {
	TypeID      types.String `tfsdk:"type_id"`
	URN         types.String `tfsdk:"urn"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

// NewOwnershipTypeDataSource returns the singular datahub_ownership_type data source.
func NewOwnershipTypeDataSource() datasource.DataSource {
	return &ownershipTypeDataSource{}
}

func (d *ownershipTypeDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ownership_type"
}

func (d *ownershipTypeDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Looks up an existing DataHub ownership type by `type_id`.\n\n" +
			"Use this to reference an ownership type -- including the built-in system types " +
			"(`__system__technical_owner`, `__system__business_owner`, `__system__data_steward`, " +
			"`__system__none`) -- without taking ownership of it in Terraform state.\n\n" +
			"Custom ownership types managed by the `datahub_ownership_type` resource can also " +
			"be looked up here if you need a cross-module URN reference.",
		Attributes: map[string]schema.Attribute{
			"type_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The unique identifier of the ownership type to look up (the URN suffix, e.g. `data_quality_lead` or `__system__technical_owner`).",
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this ownership type (e.g. `urn:li:ownershipType:data_quality_lead`).",
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Human-readable display name of the ownership type.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description of the ownership type. Empty if not set.",
			},
		},
	}
}

func (d *ownershipTypeDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ownershipTypeDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var config ownershipTypeDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	typeID := config.TypeID.ValueString()
	urn := ownershipTypeURNPrefix + typeID

	ot, err := d.client.GetOwnershipTypeByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if ot == nil {
		resp.Diagnostics.AddError(
			"Ownership type not found",
			fmt.Sprintf("No ownership type with ID %q was found in DataHub. Verify the type_id and retry.", typeID),
		)
		return
	}

	state := ownershipTypeDataSourceModel{
		TypeID:      types.StringValue(ot.ID),
		URN:         types.StringValue(ot.URN),
		Name:        types.StringValue(ot.Name),
		Description: types.StringValue(ot.Description),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
