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
	_ datasource.DataSource              = &glossaryNodeDataSource{}
	_ datasource.DataSourceWithConfigure = &glossaryNodeDataSource{}
)

type glossaryNodeDataSource struct {
	client *datahub.Client
}

type glossaryNodeDataSourceModel struct {
	NodeID      types.String `tfsdk:"node_id"`
	URN         types.String `tfsdk:"urn"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	ParentNode  types.String `tfsdk:"parent_node"`
}

// NewGlossaryNodeDataSource returns the singular datahub_glossary_node lookup
// data source.
func NewGlossaryNodeDataSource() datasource.DataSource {
	return &glossaryNodeDataSource{}
}

func (d *glossaryNodeDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_glossary_node"
}

func (d *glossaryNodeDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Looks up an existing DataHub glossary node (Term Group) by `node_id`.\n\n" +
			"Use this to reference a term group that already exists in DataHub -- for example " +
			"one created in the DataHub UI or via the Python SDK -- as a `parent_node` in a " +
			"`datahub_glossary_node` or `datahub_glossary_term` resource without taking " +
			"ownership of it.",
		Attributes: map[string]schema.Attribute{
			"node_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The unique identifier of the glossary node to look up (the URN suffix, e.g. `finance`).",
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this glossary node (e.g. `urn:li:glossaryNode:finance`).",
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Human-readable display name of the term group.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description of the term group. Empty if not set.",
			},
			"parent_node": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full URN of the parent glossary node, or empty if this is a root-level term group.",
			},
		},
	}
}

func (d *glossaryNodeDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *glossaryNodeDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var config glossaryNodeDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	nodeID := config.NodeID.ValueString()
	urn := glossaryNodeURNPrefix + nodeID

	node, err := d.client.GetGlossaryNodeByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if node == nil {
		resp.Diagnostics.AddError(
			"Glossary node not found",
			fmt.Sprintf("No glossary node with ID %q was found in DataHub. Verify the node_id and retry.", nodeID),
		)
		return
	}

	state := glossaryNodeDataSourceModel{
		NodeID:      types.StringValue(node.ID),
		URN:         types.StringValue(node.URN),
		Name:        types.StringValue(node.Name),
		Description: types.StringValue(node.Definition),
		ParentNode:  types.StringValue(node.ParentNode),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
