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
	_ datasource.DataSource              = &serviceAccountDataSource{}
	_ datasource.DataSourceWithConfigure = &serviceAccountDataSource{}
)

type serviceAccountDataSource struct {
	client *datahub.Client
}

type serviceAccountDataSourceModel struct {
	ServiceAccountID types.String `tfsdk:"service_account_id"`
	URN              types.String `tfsdk:"urn"`
	DisplayName      types.String `tfsdk:"display_name"`
	Description      types.String `tfsdk:"description"`
	Active           types.Bool   `tfsdk:"active"`
}

// NewServiceAccountDataSource returns the datahub_service_account lookup data source.
func NewServiceAccountDataSource() datasource.DataSource {
	return &serviceAccountDataSource{}
}

func (d *serviceAccountDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_account"
}

func (d *serviceAccountDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Looks up an existing DataHub service account by `service_account_id`.\n\n" +
			"Use this to resolve a service account to its URN for use as a policy actor, group " +
			"member, or ownership reference. Requires DataHub Core >= 1.4.0 or DataHub Cloud. " +
			"Fails if the id resolves to a `corpUser` that is not a service account (missing the " +
			"`SERVICE_ACCOUNT` subtype).\n\n" +
			"To manage a service account as code, see the `datahub_service_account` resource.",
		Attributes: map[string]schema.Attribute{
			"service_account_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The service account id to look up (without the `service_` prefix). Becomes the " +
					"URN `urn:li:corpuser:service_<service_account_id>`.",
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this service account (e.g. `urn:li:corpuser:service_ci-bot`).",
			},
			"display_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Display name of the service account. Empty if not set.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description of the service account (corpUser title). Empty if not set.",
			},
			"active": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the service account is active.",
			},
		},
	}
}

func (d *serviceAccountDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *serviceAccountDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var config serviceAccountDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ServiceAccountID.ValueString()
	urn := datahub.ServiceAccountURN(id)

	sa, err := d.client.GetServiceAccountByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if sa == nil {
		resp.Diagnostics.AddError(
			"Service account not found",
			fmt.Sprintf("No service account with id %q (URN %q) was found, or the entity is not a service account. Verify the id and retry.", id, urn),
		)
		return
	}

	state := serviceAccountDataSourceModel{
		ServiceAccountID: types.StringValue(datahub.ServiceAccountIDFromURN(sa.URN)),
		URN:              types.StringValue(sa.URN),
		DisplayName:      types.StringValue(sa.DisplayName),
		Description:      types.StringValue(sa.InfoTitle),
		Active:           types.BoolValue(sa.Active),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
