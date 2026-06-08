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
	_ datasource.DataSource              = &dataProductDataSource{}
	_ datasource.DataSourceWithConfigure = &dataProductDataSource{}
)

type dataProductDataSource struct {
	client *datahub.Client
}

type dataProductDataSourceModel struct {
	DataProductID    types.String `tfsdk:"data_product_id"`
	URN              types.String `tfsdk:"urn"`
	Name             types.String `tfsdk:"name"`
	Description      types.String `tfsdk:"description"`
	ExternalURL      types.String `tfsdk:"external_url"`
	CustomProperties types.Map    `tfsdk:"custom_properties"`
	Domain           types.String `tfsdk:"domain"`
}

// NewDataProductDataSource returns the singular datahub_data_product data source.
func NewDataProductDataSource() datasource.DataSource {
	return &dataProductDataSource{}
}

func (d *dataProductDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_product"
}

func (d *dataProductDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Looks up an existing DataHub data product by `data_product_id`.\n\n" +
			"Use this to reference a data product's URN in other resources (e.g. as a " +
			"`domain` reference) without taking ownership of it in Terraform state.",
		Attributes: map[string]schema.Attribute{
			"data_product_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The unique identifier of the data product to look up (the URN suffix, e.g. `orders-v2`).",
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this data product (e.g. `urn:li:dataProduct:orders-v2`).",
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Human-readable display name of the data product.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description of the data product. Empty if not set.",
			},
			"external_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "External documentation URL. Empty if not set.",
			},
			"custom_properties": schema.MapAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Custom metadata properties attached to this data product.",
			},
			"domain": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN of the owning domain, or empty if not set.",
			},
		},
	}
}

func (d *dataProductDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *dataProductDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var config dataProductDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	dataProductID := config.DataProductID.ValueString()
	urn := dataProductURNPrefix + dataProductID

	dp, err := d.client.GetDataProductByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if dp == nil {
		resp.Diagnostics.AddError(
			"Data product not found",
			fmt.Sprintf("No data product with ID %q was found in DataHub. Verify the data_product_id and retry.", dataProductID),
		)
		return
	}

	state := dataProductDataSourceModel{
		DataProductID: types.StringValue(dp.ID),
		URN:           types.StringValue(dp.URN),
		Name:          types.StringValue(dp.Name),
		Description:   types.StringValue(dp.Description),
		ExternalURL:   types.StringValue(dp.ExternalURL),
		Domain:        types.StringValue(dp.Domain),
	}

	// Convert custom_properties map.
	if len(dp.CustomProperties) > 0 {
		elems := make(map[string]interface{}, len(dp.CustomProperties))
		for k, v := range dp.CustomProperties {
			elems[k] = types.StringValue(v)
		}
		mv, diags := types.MapValueFrom(ctx, types.StringType, elems)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		state.CustomProperties = mv
	} else {
		state.CustomProperties = types.MapNull(types.StringType)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
