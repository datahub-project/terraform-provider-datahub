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
	_ datasource.DataSource              = &glossaryTermDataSource{}
	_ datasource.DataSourceWithConfigure = &glossaryTermDataSource{}
)

type glossaryTermDataSource struct {
	client *datahub.Client
}

type glossaryTermDataSourceModel struct {
	TermID      types.String `tfsdk:"term_id"`
	URN         types.String `tfsdk:"urn"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	ParentNode  types.String `tfsdk:"parent_node"`
}

// NewGlossaryTermDataSource returns the singular datahub_glossary_term lookup
// data source.
func NewGlossaryTermDataSource() datasource.DataSource {
	return &glossaryTermDataSource{}
}

func (d *glossaryTermDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_glossary_term"
}

func (d *glossaryTermDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Looks up an existing DataHub glossary term by `term_id`.\n\n" +
			"Use this to reference a term that already exists in DataHub -- for example one " +
			"created in the DataHub UI or via the Python SDK -- as a `parent_node` input or " +
			"to read its URN for use in other resources (e.g. policies).",
		Attributes: map[string]schema.Attribute{
			"term_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The unique identifier of the glossary term to look up (the URN suffix, e.g. `revenue`).",
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this glossary term (e.g. `urn:li:glossaryTerm:revenue`).",
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Human-readable display name of the term.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Definition of the term. Empty if not set.",
			},
			"parent_node": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full URN of the parent glossary node (Term Group), or empty if this term is at the root level.",
			},
		},
	}
}

func (d *glossaryTermDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *glossaryTermDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var config glossaryTermDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	termID := config.TermID.ValueString()
	urn := glossaryTermURNPrefix + termID

	term, err := d.client.GetGlossaryTermByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if term == nil {
		resp.Diagnostics.AddError(
			"Glossary term not found",
			fmt.Sprintf("No glossary term with ID %q was found in DataHub. Verify the term_id and retry.", termID),
		)
		return
	}

	state := glossaryTermDataSourceModel{
		TermID:      types.StringValue(term.ID),
		URN:         types.StringValue(term.URN),
		Name:        types.StringValue(term.Name),
		Description: types.StringValue(term.Definition),
		ParentNode:  types.StringValue(term.ParentNode),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
