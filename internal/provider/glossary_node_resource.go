// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

const glossaryNodeURNPrefix = "urn:li:glossaryNode:"

var (
	_ resource.Resource                = &glossaryNodeResource{}
	_ resource.ResourceWithConfigure   = &glossaryNodeResource{}
	_ resource.ResourceWithImportState = &glossaryNodeResource{}
)

type glossaryNodeResource struct {
	client *datahub.Client
}

type glossaryNodeResourceModel struct {
	ID               types.String `tfsdk:"id"`
	URN              types.String `tfsdk:"urn"`
	NodeID           types.String `tfsdk:"node_id"`
	Name             types.String `tfsdk:"name"`
	Description      types.String `tfsdk:"description"`
	ParentNode       types.String `tfsdk:"parent_node"`
	Domain           types.String `tfsdk:"domain"`
	CustomProperties types.Map    `tfsdk:"custom_properties"`
}

func NewGlossaryNodeResource() resource.Resource {
	return &glossaryNodeResource{}
}

func (r *glossaryNodeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*datahub.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *datahub.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	r.client = client
}

func (r *glossaryNodeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_glossary_node"
}

func (r *glossaryNodeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a DataHub glossary node (also called a **Term Group** in the " +
			"DataHub UI).\n\n" +
			"Glossary nodes are the folder-like containers of the Business Glossary hierarchy. " +
			"They can be nested inside other nodes to any depth, and glossary terms " +
			"(`datahub_glossary_term`) hang off nodes as leaves.\n\n" +
			"## Ordering\n\n" +
			"Set `parent_node` to the `.urn` attribute of another `datahub_glossary_node` " +
			"resource (rather than a raw URN string) so Terraform's dependency graph creates " +
			"parents before children and destroys children before parents.\n\n" +
			"**Note:** unlike domains, DataHub does not refuse to delete a node that still " +
			"has children -- the server will succeed, leaving child nodes or terms " +
			"parentless. Correct `parent_node = <resource>.urn` references are the only " +
			"thing that guarantees safe destroy ordering.\n\n" +
			"## Naming\n\n" +
			"`node_id` becomes the URN suffix (`urn:li:glossaryNode:<node_id>`). Supplying " +
			"an explicit, deterministic id avoids the random UUID that the DataHub UI " +
			"assigns, and keeps the URN stable and predictable.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this glossary node (e.g. `urn:li:glossaryNode:finance`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"node_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Unique identifier for the glossary node. Becomes the URN suffix " +
					"(`urn:li:glossaryNode:<node_id>`). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable display name for the term group, shown throughout the DataHub UI.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the term group's scope and purpose.",
			},
			"parent_node": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Full URN of the parent glossary node " +
					"(e.g. `urn:li:glossaryNode:finance`). Set to " +
					"`datahub_glossary_node.<name>.urn` (not a raw string) so Terraform's " +
					"dependency graph orders creation and destruction correctly. Omit for a " +
					"root-level term group. Changing this value reparents the node in place " +
					"without forcing replacement.",
			},
			"domain": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Full URN of the DataHub domain to associate with this term group " +
					"(e.g. `urn:li:domain:finance`). Set to `datahub_domain.<name>.urn` so " +
					"Terraform's dependency graph creates the domain before the term group. " +
					"Changing this updates the association in place without forcing replacement.",
			},
			"custom_properties": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Arbitrary key-value metadata attached to the term group (the " +
					"`customProperties` field of the `glossaryNodeInfo` aspect). Terraform owns the " +
					"complete map: keys added outside Terraform are removed on the next apply. Keys and " +
					"values must be non-empty strings, and values must not be null. Omit the attribute " +
					"entirely (do not set an empty map) to attach no custom properties.",
				Validators: []validator.Map{
					nonEmptyStringMapValidator{},
				},
			},
		},
	}
}

func (r *glossaryNodeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan glossaryNodeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	customProps, d := mapValToStringMap(ctx, plan.CustomProperties)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn, err := r.client.CreateGlossaryNode(ctx, datahub.CreateGlossaryEntityInput{
		ID:         plan.NodeID.ValueString(),
		Name:       plan.Name.ValueString(),
		Definition: strVal(plan.Description),
		ParentNode: strVal(plan.ParentNode),
	})
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = types.StringValue(urn)
	plan.URN = types.StringValue(urn)

	if domain := strVal(plan.Domain); domain != "" {
		if err := r.client.SetEntityDomain(ctx, urn, domain); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	// custom_properties is not carried by the GraphQL createGlossaryNode mutation,
	// so write it via the OpenAPI v3 entity endpoint (carrying name/definition/
	// parentNode to avoid clobbering what createGlossaryNode just set).
	if len(customProps) > 0 {
		if err := r.client.SetGlossaryNodeProperties(ctx, urn, plan.Name.ValueString(), strVal(plan.Description), strVal(plan.ParentNode), customProps); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *glossaryNodeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state glossaryNodeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}

	node, err := r.client.GetGlossaryNodeByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if node == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	applyGlossaryNodeToModel(node, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *glossaryNodeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan, state glossaryNodeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()

	if plan.Name.ValueString() != state.Name.ValueString() {
		if err := r.client.UpdateEntityName(ctx, urn, plan.Name.ValueString()); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	if strVal(plan.Description) != strVal(state.Description) {
		if err := r.client.UpdateEntityDescription(ctx, urn, strVal(plan.Description)); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	if strVal(plan.ParentNode) != strVal(state.ParentNode) {
		if err := r.client.MoveGlossaryEntity(ctx, urn, strVal(plan.ParentNode)); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	if strVal(plan.Domain) != strVal(state.Domain) {
		if domain := strVal(plan.Domain); domain != "" {
			if err := r.client.SetEntityDomain(ctx, urn, domain); err != nil {
				resp.Diagnostics.AddError("DataHub API Error", err.Error())
				return
			}
		} else {
			if err := r.client.UnsetEntityDomain(ctx, urn); err != nil {
				resp.Diagnostics.AddError("DataHub API Error", err.Error())
				return
			}
		}
	}

	// Write custom_properties via OpenAPI v3 when it changed (including clearing
	// it: an empty map overwrites a previously-set value). Pass the plan's
	// name/definition/parentNode so the glossaryNodeInfo aspect write does not
	// clobber the values the GraphQL mutations above just applied.
	if !plan.CustomProperties.Equal(state.CustomProperties) {
		customProps, d := mapValToStringMap(ctx, plan.CustomProperties)
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		if err := r.client.SetGlossaryNodeProperties(ctx, urn, plan.Name.ValueString(), strVal(plan.Description), strVal(plan.ParentNode), customProps); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	plan.ID = state.ID
	plan.URN = state.URN
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *glossaryNodeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state glossaryNodeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}
	if urn == "" {
		resp.State.RemoveResource(ctx)
		return
	}

	if err := r.client.DeleteGlossaryEntity(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *glossaryNodeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub glossary node URN (e.g., urn:li:glossaryNode:finance) or a bare node ID.")
		return
	}

	var nodeID, urn string
	if strings.HasPrefix(raw, glossaryNodeURNPrefix) {
		urn = raw
		nodeID = strings.TrimPrefix(raw, glossaryNodeURNPrefix)
	} else {
		nodeID = raw
		urn = glossaryNodeURNPrefix + nodeID
	}
	if nodeID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub glossary node URN (e.g., urn:li:glossaryNode:finance) or a bare node ID.")
		return
	}

	node, err := r.client.GetGlossaryNodeByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if node == nil {
		resp.Diagnostics.AddError(
			"Glossary node not found",
			fmt.Sprintf("No glossary node with URN %q was found in DataHub. Verify the ID or URN and retry.", urn),
		)
		return
	}

	state := glossaryNodeResourceModel{
		ID:     types.StringValue(node.URN),
		URN:    types.StringValue(node.URN),
		NodeID: types.StringValue(node.ID),
	}
	applyGlossaryNodeToModel(node, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// applyGlossaryNodeToModel maps a read GlossaryNode onto the model, normalising
// empty optional fields to null so they do not show spurious drift.
func applyGlossaryNodeToModel(node *datahub.GlossaryNode, m *glossaryNodeResourceModel) {
	m.URN = types.StringValue(node.URN)
	m.ID = types.StringValue(node.URN)
	m.Name = types.StringValue(node.Name)
	m.Description = nullIfEmpty(node.Definition)
	m.ParentNode = nullIfEmpty(node.ParentNode)
	m.Domain = nullIfEmpty(node.Domain)
	m.CustomProperties = stringMapToTfMap(node.CustomProperties)
}
