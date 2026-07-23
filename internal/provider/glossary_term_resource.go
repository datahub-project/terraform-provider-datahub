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

const glossaryTermURNPrefix = "urn:li:glossaryTerm:"

var (
	_ resource.Resource                = &glossaryTermResource{}
	_ resource.ResourceWithConfigure   = &glossaryTermResource{}
	_ resource.ResourceWithImportState = &glossaryTermResource{}
	_ resource.ResourceWithModifyPlan  = &glossaryTermResource{}
)

type glossaryTermResource struct {
	pd       *providerData
	client   *datahub.Client
	defaults entityDefaults
}

type glossaryTermResourceModel struct {
	ID                           types.String `tfsdk:"id"`
	URN                          types.String `tfsdk:"urn"`
	TermID                       types.String `tfsdk:"term_id"`
	Name                         types.String `tfsdk:"name"`
	Description                  types.String `tfsdk:"description"`
	ParentNode                   types.String `tfsdk:"parent_node"`
	Domain                       types.String `tfsdk:"domain"`
	CustomProperties             types.Map    `tfsdk:"custom_properties"`
	CustomPropertiesAll          types.Map    `tfsdk:"custom_properties_all"`
	StructuredPropertiesDefaults types.Map    `tfsdk:"structured_properties_defaults"`
}

func NewGlossaryTermResource() resource.Resource {
	return &glossaryTermResource{}
}

func (r *glossaryTermResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	r.pd = pd
	r.client = pd.Client
	r.defaults = pd.defaults
}

func (r *glossaryTermResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy plan
	}
	if r.pd == nil {
		return
	}
	planCustomPropertiesAll(ctx, r.defaults, req, resp)
	planSPDefaults(ctx, r.defaults, r.pd.spDefs, kindGlossaryTerm, resp)
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
			"coexisting with SDK-created terms. The DataHub model caps the id at 56 characters.\n\n" +
			"## Orphaned-husk repair\n\n" +
			"A DataHub server bug can leave an invisible, empty \"husk\" entity behind when a " +
			"structured property and entities carrying it are deleted around the same time " +
			"(e.g. one `terraform destroy`), which then blocks re-creation with an " +
			"\"already exists\" error. When create hits that error and the blocking entity is " +
			"provably such a husk (no info aspect, no data beyond an empty structured-properties " +
			"aspect), the provider removes the husk, retries the create, and reports a warning. " +
			"Entities with any real content are never touched.",
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
			"custom_properties": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Arbitrary key-value metadata attached to the term (the " +
					"`customProperties` field of the `glossaryTermInfo` aspect). Terraform owns the " +
					"complete map: keys added outside Terraform are removed on the next apply. Keys and " +
					"values must be non-empty strings, and values must not be null. Omit the attribute " +
					"entirely (do not set an empty map) to attach no custom properties. Provider-level " +
					"defaults (`auto_properties` markers and `defaults.custom_properties`) are merged in " +
					"automatically; the effective written map is the computed `custom_properties_all`.",
				Validators: []validator.Map{
					nonEmptyStringMapValidator{},
				},
			},
			"custom_properties_all":          customPropertiesAllSchema(),
			"structured_properties_defaults": spDefaultsSchema(),
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

	all, customProps, d := resolvePlannedCustomPropertiesAll(ctx, r.defaults, plan.CustomPropertiesAll, plan.CustomProperties, types.MapNull(types.StringType), true)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.CustomPropertiesAll = all

	urn, repairedHusk, err := r.client.CreateGlossaryTerm(ctx, datahub.CreateGlossaryEntityInput{
		ID:         plan.TermID.ValueString(),
		Name:       plan.Name.ValueString(),
		Definition: strVal(plan.Description),
		ParentNode: strVal(plan.ParentNode),
	})
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if repairedHusk {
		resp.Diagnostics.AddWarning(
			"Repaired orphaned glossary term",
			fmt.Sprintf("An empty glossary term husk existed at %s - debris left by DataHub's "+
				"structured-property cleanup writing to a hard-deleted entity (CAT-2583). "+
				"The provider removed it and created the term normally.", urn),
		)
	}

	plan.ID = types.StringValue(urn)
	plan.URN = types.StringValue(urn)

	if domain := strVal(plan.Domain); domain != "" {
		if err := r.client.SetEntityDomain(ctx, urn, domain); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	// custom_properties is not carried by the GraphQL createGlossaryTerm mutation,
	// so write it via the OpenAPI v3 entity endpoint (carrying name/definition/
	// parentNode to avoid clobbering what createGlossaryTerm just set).
	if len(customProps) > 0 {
		if err := r.client.SetGlossaryTermProperties(ctx, urn, plan.Name.ValueString(), strVal(plan.Description), strVal(plan.ParentNode), customProps); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	spAll, spVals, sd := resolvePlannedSPDefaults(r.defaults, r.pd.spDefs, kindGlossaryTerm, plan.StructuredPropertiesDefaults)
	resp.Diagnostics.Append(sd...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.StructuredPropertiesDefaults = spAll
	if len(spVals) > 0 {
		resp.Diagnostics.Append(applySPDefaults(ctx, r.pd, urn, spVals, types.MapNull(spDefaultsElementType))...)
		if resp.Diagnostics.HasError() {
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
	spDefaults, err := readSPDefaults(ctx, r.client, urn, state.StructuredPropertiesDefaults)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	state.StructuredPropertiesDefaults = spDefaults
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

	// Write custom properties via OpenAPI v3 when the effective merged map
	// (custom_properties_all) changed, including clearing it: an empty map
	// overwrites a previously-set value. Pass the plan's name/definition/
	// parentNode so the glossaryTermInfo aspect write does not clobber the
	// values the GraphQL mutations above just applied.
	if !plan.CustomPropertiesAll.Equal(state.CustomPropertiesAll) {
		all, customProps, d := resolvePlannedCustomPropertiesAll(ctx, r.defaults, plan.CustomPropertiesAll, plan.CustomProperties, state.CustomPropertiesAll, false)
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.CustomPropertiesAll = all
		if err := r.client.SetGlossaryTermProperties(ctx, urn, plan.Name.ValueString(), strVal(plan.Description), strVal(plan.ParentNode), customProps); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	// Reconcile default structured properties when the managed set changed.
	if !plan.StructuredPropertiesDefaults.Equal(state.StructuredPropertiesDefaults) {
		spAll, spVals, sd := resolvePlannedSPDefaults(r.defaults, r.pd.spDefs, kindGlossaryTerm, plan.StructuredPropertiesDefaults)
		resp.Diagnostics.Append(sd...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.StructuredPropertiesDefaults = spAll
		resp.Diagnostics.Append(applySPDefaults(ctx, r.pd, urn, spVals, state.StructuredPropertiesDefaults)...)
		if resp.Diagnostics.HasError() {
			return
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
	// Import attribution: keys matching provider defaults or auto-property
	// markers are omitted from custom_properties so the first plan after
	// import is minimal.
	state.CustomProperties, state.CustomPropertiesAll = importCustomProperties(term.CustomProperties, r.defaults)
	spDefaults, err := importSPDefaults(ctx, r.client, r.defaults, r.pd.spDefs, kindGlossaryTerm, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	state.StructuredPropertiesDefaults = spDefaults
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// applyGlossaryTermToModel maps a read GlossaryTerm onto the model, normalising
// empty optional fields to null so they do not show spurious drift. Custom
// properties are reconciled against the model's prior state: the user-facing
// attribute keeps only its own keys, the computed _all records the full
// server map.
func applyGlossaryTermToModel(term *datahub.GlossaryTerm, m *glossaryTermResourceModel) {
	m.URN = types.StringValue(term.URN)
	m.ID = types.StringValue(term.URN)
	m.Name = types.StringValue(term.Name)
	m.Description = nullIfEmpty(term.Definition)
	m.ParentNode = nullIfEmpty(term.ParentNode)
	m.Domain = nullIfEmpty(term.Domain)
	m.CustomProperties, m.CustomPropertiesAll = reconcileCustomPropertiesRead(term.CustomProperties, m.CustomProperties)
}
