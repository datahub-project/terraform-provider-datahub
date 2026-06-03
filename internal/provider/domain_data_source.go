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
	_ datasource.DataSource              = &domainDataSource{}
	_ datasource.DataSourceWithConfigure = &domainDataSource{}
)

type domainDataSource struct {
	client *datahub.Client
}

type domainDataSourceModel struct {
	DomainID     types.String `tfsdk:"domain_id"`
	URN          types.String `tfsdk:"urn"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	ParentDomain types.String `tfsdk:"parent_domain"`
}

// NewDomainDataSource returns the singular datahub_domain lookup data source.
func NewDomainDataSource() datasource.DataSource {
	return &domainDataSource{}
}

func (d *domainDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (d *domainDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Looks up an existing DataHub domain by `domain_id`.\n\n" +
			"Use this to reference a domain that already exists in DataHub -- for example one " +
			"created in the DataHub UI or via the Python SDK -- as a `parent_domain` in a " +
			"`datahub_domain` resource without taking ownership of it.",
		Attributes: map[string]schema.Attribute{
			"domain_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The unique identifier of the domain to look up (the URN suffix, e.g. `marketing`).",
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this domain (e.g. `urn:li:domain:marketing`).",
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Human-readable display name of the domain.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description of the domain. Empty if not set.",
			},
			"parent_domain": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full URN of the parent domain, or empty if this is a root domain.",
			},
		},
	}
}

func (d *domainDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *domainDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var config domainDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domainID := config.DomainID.ValueString()
	urn := domainURNPrefix + domainID

	domain, err := d.client.GetDomainByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if domain == nil {
		resp.Diagnostics.AddError(
			"Domain not found",
			fmt.Sprintf("No domain with ID %q was found in DataHub. Verify the domain_id and retry.", domainID),
		)
		return
	}

	state := domainDataSourceModel{
		DomainID:     types.StringValue(domain.ID),
		URN:          types.StringValue(domain.URN),
		Name:         types.StringValue(domain.Name),
		Description:  types.StringValue(domain.Description),
		ParentDomain: types.StringValue(domain.ParentDomain),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
