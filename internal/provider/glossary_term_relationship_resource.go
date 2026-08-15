// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// termRelationshipIDSeparator joins the three edge components into the
// composite resource id: <term_urn>|<relationship_type>|<related_term_urn>.
// glossaryTermURNPrefix is declared in glossary_term_resource.go.
const termRelationshipIDSeparator = "|"

var (
	_ resource.Resource                   = &glossaryTermRelationshipResource{}
	_ resource.ResourceWithConfigure      = &glossaryTermRelationshipResource{}
	_ resource.ResourceWithImportState    = &glossaryTermRelationshipResource{}
	_ resource.ResourceWithValidateConfig = &glossaryTermRelationshipResource{}
)

// glossaryTermURNValidator rejects a string attribute that is not a
// glossaryTerm URN. Null and unknown values are skipped (an unknown carries
// nothing to check; see the design doc's "Unknown Values" section).
type glossaryTermURNValidator struct{}

func (v glossaryTermURNValidator) Description(_ context.Context) string {
	return fmt.Sprintf("must be a glossary term URN starting with %q", glossaryTermURNPrefix)
}

func (v glossaryTermURNValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v glossaryTermURNValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	got := req.ConfigValue.ValueString()
	if !strings.HasPrefix(got, glossaryTermURNPrefix) || got == glossaryTermURNPrefix {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid glossary term URN",
			fmt.Sprintf("%q is not a glossary term URN; expected a URN starting with %q.", got, glossaryTermURNPrefix),
		)
	}
}

type glossaryTermRelationshipResource struct {
	client *datahub.Client
}

type glossaryTermRelationshipResourceModel struct {
	ID               types.String `tfsdk:"id"`
	TermURN          types.String `tfsdk:"term_urn"`
	RelationshipType types.String `tfsdk:"relationship_type"`
	RelatedTermURN   types.String `tfsdk:"related_term_urn"`
}

func NewGlossaryTermRelationshipResource() resource.Resource {
	return &glossaryTermRelationshipResource{}
}

func (r *glossaryTermRelationshipResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	r.client = pd.Client
}

func (r *glossaryTermRelationshipResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_glossary_term_relationship"
}

func (r *glossaryTermRelationshipResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Manages a single typed relationship edge between two glossary terms: `term_urn` " +
			"*inherits from* (`isA`) or *contains* (`hasA`) `related_term_urn`.\n\n" +
			"Following the HashiCorp idiom (as `datahub_corp_group_member` does for group " +
			"membership), each relationship is its own resource rather than a list on " +
			"`datahub_glossary_term`. Relationships added outside Terraform are left untouched: " +
			"this resource owns only its single edge.\n\n" +
			"The edge is directional and stored one-sided, on `term_urn`'s `glossaryRelatedTerms` " +
			"aspect -- DataHub writes no inverse edge on `related_term_urn`. In the DataHub UI, " +
			"`related_term_urn` appears under `term_urn`'s **Inherits** (`isA`) or **Contains** " +
			"(`hasA`) section, and `term_urn` appears under `related_term_urn`'s **Inherited by** " +
			"or **Contained by** section (a reverse graph lookup, not a second edge).\n\n" +
			"Unlike most DataHub references, related terms are validated referentially at write " +
			"time: both terms must already exist or DataHub rejects the edge.\n\n" +
			"## References\n\n" +
			"Prefer expression inputs so Terraform orders operations correctly: set `term_urn` " +
			"and `related_term_urn` to `datahub_glossary_term.<name>.urn`. Raw URN strings work " +
			"for terms managed outside Terraform, but then you are responsible for the terms " +
			"existing before the apply.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Composite identifier: `<term_urn>|<relationship_type>|<related_term_urn>`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"term_urn": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "URN of the source glossary term -- the term that inherits from or contains the related term. The edge is stored on this term. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					glossaryTermURNValidator{},
				},
			},
			"relationship_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The relationship type, verbatim from DataHub's `TermRelationshipType` enum: `isA` (the source term inherits from the related term; \"Inherits\" in the UI) or `hasA` (the source term contains the related term; \"Contains\" in the UI). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					enumString(datahub.TermRelationshipTypes()...),
				},
			},
			"related_term_urn": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "URN of the related glossary term -- the term being inherited from (`isA`) or contained (`hasA`). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					glossaryTermURNValidator{},
				},
			},
		},
	}
}

// ValidateConfig rejects a self-relationship at plan time (the server rejects
// it at apply time with the same meaning). Skipped when either URN is null or
// unknown: an unknown carries nothing to compare.
func (r *glossaryTermRelationshipResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config glossaryTermRelationshipResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.TermURN.IsNull() || config.TermURN.IsUnknown() ||
		config.RelatedTermURN.IsNull() || config.RelatedTermURN.IsUnknown() {
		return
	}
	if config.TermURN.ValueString() == config.RelatedTermURN.ValueString() {
		resp.Diagnostics.AddAttributeError(
			path.Root("related_term_urn"),
			"Self-relationship not allowed",
			"term_urn and related_term_urn are the same term. DataHub rejects a glossary term related to itself.",
		)
	}
}

func termRelationshipID(termURN, relationshipType, relatedTermURN string) string {
	return termURN + termRelationshipIDSeparator + relationshipType + termRelationshipIDSeparator + relatedTermURN
}

func (r *glossaryTermRelationshipResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan glossaryTermRelationshipResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	termURN := plan.TermURN.ValueString()
	relationshipType := plan.RelationshipType.ValueString()
	relatedTermURN := plan.RelatedTermURN.ValueString()

	if err := r.client.AddRelatedTerm(ctx, termURN, relationshipType, relatedTermURN); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	// Confirm the edge landed (guards against a silent no-op).
	exists, err := r.client.RelatedTermExists(ctx, termURN, relationshipType, relatedTermURN)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if !exists {
		resp.Diagnostics.AddError(
			"Glossary term relationship did not take effect",
			fmt.Sprintf("After relating %q to %q (%s), the edge was not present on read back. Verify both terms exist in DataHub.", termURN, relatedTermURN, relationshipType),
		)
		return
	}

	plan.ID = types.StringValue(termRelationshipID(termURN, relationshipType, relatedTermURN))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *glossaryTermRelationshipResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state glossaryTermRelationshipResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	termURN := state.TermURN.ValueString()
	relationshipType := state.RelationshipType.ValueString()
	relatedTermURN := state.RelatedTermURN.ValueString()

	exists, err := r.client.RelatedTermExists(ctx, termURN, relationshipType, relatedTermURN)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(termRelationshipID(termURN, relationshipType, relatedTermURN))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is unreachable: every attribute forces replacement. Implemented to
// satisfy the resource.Resource interface.
func (r *glossaryTermRelationshipResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan glossaryTermRelationshipResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *glossaryTermRelationshipResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state glossaryTermRelationshipResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// RemoveRelatedTerm is idempotent: a source term or edge already gone
	// (e.g. the term was deleted out-of-band) is a successful delete.
	if err := r.client.RemoveRelatedTerm(ctx, state.TermURN.ValueString(), state.RelationshipType.ValueString(), state.RelatedTermURN.ValueString()); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *glossaryTermRelationshipResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(strings.TrimSpace(req.ID), termRelationshipIDSeparator)
	if len(parts) != 3 {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			"Expected a composite ID of the form \"<term_urn>|<relationship_type>|<related_term_urn>\" "+
				"(e.g. urn:li:glossaryTerm:pii|isA|urn:li:glossaryTerm:classification).",
		)
		return
	}
	termURN := strings.TrimSpace(parts[0])
	relationshipType := strings.TrimSpace(parts[1])
	relatedTermURN := strings.TrimSpace(parts[2])

	if !strings.HasPrefix(termURN, glossaryTermURNPrefix) || !strings.HasPrefix(relatedTermURN, glossaryTermURNPrefix) {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Both term URNs must start with %q.", glossaryTermURNPrefix),
		)
		return
	}
	validType := false
	for _, t := range datahub.TermRelationshipTypes() {
		if relationshipType == t {
			validType = true
			break
		}
	}
	if !validType {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Relationship type %q is not valid; expected one of: %s.", relationshipType, strings.Join(datahub.TermRelationshipTypes(), ", ")),
		)
		return
	}

	exists, err := r.client.RelatedTermExists(ctx, termURN, relationshipType, relatedTermURN)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if !exists {
		resp.Diagnostics.AddError(
			"Glossary term relationship not found",
			fmt.Sprintf("Term %q has no %s relationship to %q in DataHub.", termURN, relationshipType, relatedTermURN),
		)
		return
	}

	state := glossaryTermRelationshipResourceModel{
		ID:               types.StringValue(termRelationshipID(termURN, relationshipType, relatedTermURN)),
		TermURN:          types.StringValue(termURN),
		RelationshipType: types.StringValue(relationshipType),
		RelatedTermURN:   types.StringValue(relatedTermURN),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
