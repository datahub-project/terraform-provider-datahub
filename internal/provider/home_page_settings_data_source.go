// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ datasource.DataSource              = &homePageSettingsDataSource{}
	_ datasource.DataSourceWithConfigure = &homePageSettingsDataSource{}
)

type homePageSettingsDataSource struct {
	client *datahub.Client
}

type homePageSettingsDataSourceModel struct {
	DefaultTemplateURN types.String `tfsdk:"default_template_urn"`
	DefaultTemplateID  types.String `tfsdk:"default_template_id"`
}

// NewHomePageSettingsDataSource returns the singleton datahub_home_page_settings
// data source.
//
// Read-only by design, and there is deliberately no matching resource: DataHub
// seeds this pointer at bootstrap and exposes no way to move it, so the home
// page is changed by editing the template it names rather than by repointing.
// See docs/design/provider-home-page-layout.md.
func NewHomePageSettingsDataSource() datasource.DataSource {
	return &homePageSettingsDataSource{}
}

func (d *homePageSettingsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_home_page_settings"
}

func (d *homePageSettingsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Reads which page template DataHub renders as the home page for every user in the " +
			"organisation.\n\n" +
			"Takes no arguments: there is one home-page setting per instance.\n\n" +
			"## Why this exists\n\n" +
			"Changing the home page means editing the template this points at -- DataHub provides " +
			"no API to aim it somewhere else, and its own UI edits the default template in " +
			"place. Every instance is seeded with `urn:li:dataHubPageTemplate:home_default_1`, " +
			"but that is a bootstrap value rather than an API guarantee, so read it here instead " +
			"of hardcoding it:\n\n" +
			"```terraform\n" +
			"data \"datahub_home_page_settings\" \"current\" {}\n\n" +
			"resource \"datahub_page_template\" \"home\" {\n" +
			"  page_template_id = data.datahub_home_page_settings.current.default_template_id\n" +
			"  rows             = [/* ... */]\n" +
			"}\n" +
			"```\n\n" +
			"Managing that template means adopting an entity DataHub created. " +
			"`datahub_page_template` handles that: `terraform destroy` restores the layout the " +
			"template had beforehand rather than deleting it, so the organisation is never left " +
			"without a home page.\n\n" +
			"Both attributes are `null` on an instance with no default template set.",
		Attributes: map[string]schema.Attribute{
			"default_template_urn": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Full URN of the organisation's default home-page template, " +
					"e.g. `urn:li:dataHubPageTemplate:home_default_1`.",
			},
			"default_template_id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The URN's id suffix, ready to pass to a " +
					"`datahub_page_template` resource's `page_template_id`.",
			},
		},
	}
}

func (d *homePageSettingsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *homePageSettingsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	urn, err := d.client.GetDefaultHomePageTemplateURN(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read home page settings", err.Error())
		return
	}

	state := homePageSettingsDataSourceModel{
		DefaultTemplateURN: types.StringNull(),
		DefaultTemplateID:  types.StringNull(),
	}
	if urn != "" {
		state.DefaultTemplateURN = types.StringValue(urn)
		state.DefaultTemplateID = types.StringValue(strings.TrimPrefix(urn, datahub.PageTemplateURNPrefix))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
