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
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

const glossaryTermURNPrefix = "urn:li:glossaryTerm:"

var (
	_ resource.Resource                = &glossaryTermResource{}
	_ resource.ResourceWithConfigure   = &glossaryTermResource{}
	_ resource.ResourceWithImportState = &glossaryTermResource{}
)

type glossaryTermResource struct {
	client *datahub.Client
}

type glossaryTermResourceModel struct {
	ID          types.String `tfsdk:"id"`
	URN         types.String `tfsdk:"urn"`
	TermID      types.String `tfsdk:"term_id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	ParentNode  types.String `tfsdk:"parent_node"`
	Domain      types.String `tfsdk:"domain"`
}

func NewGlossaryTermResource() resource.Resource {
	return &glossaryTermResource{}
}

func (r *glossaryTermResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *glossaryTermResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_glossary_term"
}

func (r *glossaryTermResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a DataHub glossary term (also called a **Term** in the " +
			"DataHub UI).\n\n" +
			"Glossary terms are the leaf concepts of the Business Glossary. Each term lives " +
			"under a glossary node (`datahub_glossary_node`), which provides the folder-like " +
			"hierarchy. Terms cannot be parents of other terms.\n\n" +
			"## Ordering\n\n" +
			"Set `parent_node` to the `.urn` attribute of a `datahub_glossary_node` resource " +
			"(rather than a raw URN string) so Terraform's dependency graph creates the parent " +
			"node before the term and destroys the term before the node.\n\n" +
			"**Note:** unlike domains, DataHub does not refuse to delete a glossary node that " +
			"still has children -- the server will succeed, leaving terms parentless. " +
			"Correct `parent_node = datahub_glossary_node.<name>.urn` references are the " +
			"only thing that guarantees safe destroy ordering.\n\n" +
			"## Naming\n\n" +
			"`term_id` becomes the URN suffix (`urn:li:glossaryTerm:<term_id>`). Supplying " +
			"an explicit, deterministic id avoids the random UUID that the DataHub UI assigns, " +
			"keeps the URN stable and predictable, and prevents duplicate entities when " +
			"coexisting with SDK-created terms. The DataHub model caps the id at 56 characters.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this glossary term (e.g. `urn:li:glossaryTerm:revenue`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"term_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Unique identifier for the glossary term. Becomes the URN suffix " +
					"(`urn:li:glossaryTerm:<term_id>`). Changing this forces a new resource. " +
					"Maximum 56 characters.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable display name for the term, shown throughout the DataHub UI.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Definition of the term's meaning and scope.",
			},
			"parent_node": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Full URN of the parent glossary node (Term Group), e.g. " +
					"`urn:li:glossaryNode:finance`. Set to " +
					"`datahub_glossary_node.<name>.urn` (not a raw string) so Terraform's " +
					"dependency graph orders creation and destruction correctly. The parent " +
					"must be a glossary node -- terms cannot be parents of other terms. " +
					"Omit to place the term at the root level. Changing this value reparents " +
					"the term in place without forcing replacement.",
			},
			"domain": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Full URN of the DataHub domain to associate with this term " +
					"(e.g. `urn:li:domain:finance`). Set to `datahub_domain.<name>.urn` so " +
					"Terraform's dependency graph creates the domain before the term. " +
					"Changing this updates the association in place without forcing replacement.",
			},
		},
	}
}

func (r *glossaryTermResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan glossaryTermResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn, err := r.client.CreateGlossaryTerm(ctx, datahub.CreateGlossaryEntityInput{
		ID:         plan.TermID.ValueString(),
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

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *glossaryTermResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state glossaryTermResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}

	term, err := r.client.GetGlossaryTermByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if term == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	applyGlossaryTermToModel(term, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *glossaryTermResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan, state glossaryTermResourceModel
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

	plan.ID = state.ID
	plan.URN = state.URN
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *glossaryTermResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state glossaryTermResourceModel
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

func (r *glossaryTermResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub glossary term URN (e.g., urn:li:glossaryTerm:revenue) or a bare term ID.")
		return
	}

	var termID, urn string
	if strings.HasPrefix(raw, glossaryTermURNPrefix) {
		urn = raw
		termID = strings.TrimPrefix(raw, glossaryTermURNPrefix)
	} else {
		termID = raw
		urn = glossaryTermURNPrefix + termID
	}
	if termID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub glossary term URN (e.g., urn:li:glossaryTerm:revenue) or a bare term ID.")
		return
	}

	term, err := r.client.GetGlossaryTermByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if term == nil {
		resp.Diagnostics.AddError(
			"Glossary term not found",
			fmt.Sprintf("No glossary term with URN %q was found in DataHub. Verify the ID or URN and retry.", urn),
		)
		return
	}

	state := glossaryTermResourceModel{
		ID:     types.StringValue(term.URN),
		URN:    types.StringValue(term.URN),
		TermID: types.StringValue(term.ID),
	}
	applyGlossaryTermToModel(term, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// applyGlossaryTermToModel maps a read GlossaryTerm onto the model, normalising
// empty optional fields to null so they do not show spurious drift.
func applyGlossaryTermToModel(term *datahub.GlossaryTerm, m *glossaryTermResourceModel) {
	m.URN = types.StringValue(term.URN)
	m.ID = types.StringValue(term.URN)
	m.Name = types.StringValue(term.Name)
	m.Description = nullIfEmpty(term.Definition)
	m.ParentNode = nullIfEmpty(term.ParentNode)
	m.Domain = nullIfEmpty(term.Domain)
}
