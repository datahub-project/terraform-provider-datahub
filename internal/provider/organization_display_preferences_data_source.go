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
	_ datasource.DataSource              = &organizationDisplayPreferencesDataSource{}
	_ datasource.DataSourceWithConfigure = &organizationDisplayPreferencesDataSource{}
)

type organizationDisplayPreferencesDataSource struct {
	client *datahub.Client
}

type organizationDisplayPreferencesDataSourceModel struct {
	URN     types.String `tfsdk:"urn"`
	OrgName types.String `tfsdk:"org_name"`
	LogoURL types.String `tfsdk:"logo_url"`
}

// NewOrganizationDisplayPreferencesDataSource returns the singleton
// datahub_organization_display_preferences data source.
func NewOrganizationDisplayPreferencesDataSource() datasource.DataSource {
	return &organizationDisplayPreferencesDataSource{}
}

func (d *organizationDisplayPreferencesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_display_preferences"
}

func (d *organizationDisplayPreferencesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: cloudOnlyBadge +
			"Reads the organization-wide display preferences shown in DataHub under " +
			"**Settings -> Preferences** (the organization name and logo that brand the UI), " +
			"without taking ownership of them in Terraform state.\n\n" +
			"Takes no arguments: DataHub stores these settings on a single global settings " +
			"object, so there is exactly one set per instance. Useful for reusing the " +
			"organization name elsewhere in a configuration, or for inspecting current branding " +
			"before adopting the `datahub_organization_display_preferences` resource.\n\n" +
			"A preference that has not been set is returned as `null`.\n\n" +
			"Organization display preferences are a DataHub Cloud capability. DataHub Cloud upgrades " +
			"on its own release cadence, so a release may occasionally affect this data source; fixes " +
			"are handled in the provider. Pin the provider version for client-side stability and " +
			"upgrade it to pick up fixes (including any needed for backend changes), and please " +
			"open an issue if you hit one.",
		Attributes: map[string]schema.Attribute{
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "URN of the DataHub global settings singleton (`" + datahub.GlobalSettingsURN + "`).",
			},
			"org_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Organization name used to brand the DataHub UI, or `null` when not set.",
			},
			"logo_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "URL of the organization logo shown in the DataHub UI, or `null` when not set.",
			},
		},
	}
}

func (d *organizationDisplayPreferencesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *organizationDisplayPreferencesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	prefs, found, err := d.client.GetOrganizationDisplayPreferences(ctx)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Global settings not found",
			"The DataHub global settings object was not found on this instance, so organization "+
				"display preferences could not be read.",
		)
		return
	}

	state := organizationDisplayPreferencesDataSourceModel{
		URN:     types.StringValue(datahub.GlobalSettingsURN),
		OrgName: optionalStringValue(prefs.OrgName),
		LogoURL: optionalStringValue(prefs.LogoURL),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
