// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/tools/uid"
)

var (
	_ resource.Resource                   = &formResource{}
	_ resource.ResourceWithConfigure      = &formResource{}
	_ resource.ResourceWithImportState    = &formResource{}
	_ resource.ResourceWithValidateConfig = &formResource{}
)

// formTypes is the FormType enum.
var formTypes = []string{"COMPLETION", "VERIFICATION"}

// formPromptTypes is the FormPromptType enum.
var formPromptTypes = []string{"STRUCTURED_PROPERTY", "FIELDS_STRUCTURED_PROPERTY"}

type formResource struct {
	client *datahub.Client
}

type formResourceModel struct {
	ID                types.String `tfsdk:"id"`
	URN               types.String `tfsdk:"urn"`
	FormID            types.String `tfsdk:"form_id"`
	Name              types.String `tfsdk:"name"`
	Description       types.String `tfsdk:"description"`
	Type              types.String `tfsdk:"type"`
	Prompts           types.List   `tfsdk:"prompts"`
	Actors            types.Object `tfsdk:"actors"`
	DynamicAssignment types.Object `tfsdk:"dynamic_assignment"`
}

type formPromptModel struct {
	ID                    types.String `tfsdk:"id"`
	Title                 types.String `tfsdk:"title"`
	Description           types.String `tfsdk:"description"`
	Type                  types.String `tfsdk:"type"`
	StructuredPropertyURN types.String `tfsdk:"structured_property_urn"`
	Required              types.Bool   `tfsdk:"required"`
}

type formActorsModel struct {
	Owners types.Bool `tfsdk:"owners"`
	Users  types.List `tfsdk:"users"`
	Groups types.List `tfsdk:"groups"`
}

type formDynamicAssignmentModel struct {
	OrFilters types.List `tfsdk:"or_filters"`
}

var formPromptObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"id":                      types.StringType,
	"title":                   types.StringType,
	"description":             types.StringType,
	"type":                    types.StringType,
	"structured_property_urn": types.StringType,
	"required":                types.BoolType,
}}

var formActorsObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"owners": types.BoolType,
	"users":  types.ListType{ElemType: types.StringType},
	"groups": types.ListType{ElemType: types.StringType},
}}

var formDynamicAssignmentObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"or_filters": types.ListType{ElemType: andGroupObjectType},
}}

// NewFormResource returns a new datahub_form resource.
func NewFormResource() resource.Resource {
	return &formResource{}
}

func (r *formResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	r.client = pd.Client
}

func (r *formResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_form"
}

func (r *formResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a DataHub **form** -- a metadata-collection questionnaire " +
			"(`COMPLETION`) or compliance check (`VERIFICATION`) whose prompts ask asset owners to " +
			"fill in structured property values. The optional `dynamic_assignment` block assigns the " +
			"form automatically to every entity matching a search filter.\n\n" +
			"The calling principal needs the **Manage Metadata Forms** (`MANAGE_FORMS`) platform " +
			"privilege.\n\n" +
			"## Full-state ownership\n\n" +
			"This resource owns the complete form definition: the prompts list, the actors lists and " +
			"the dynamic assignment filter are fully replaced on every apply, so prompts or actor " +
			"assignments added outside Terraform (UI, SDK) are removed on the next apply. Omitting " +
			"`dynamic_assignment` removes any dynamic assignment on the form.\n\n" +
			"## Assignment is asynchronous and additive\n\n" +
			"DataHub assigns the form to matching entities via a background hook after the filter is " +
			"written. Narrowing or removing the filter stops *future* assignment but does not retract " +
			"the form from entities that already carry it; deleting the form does (asynchronously).\n\n" +
			"## Delete semantics\n\n" +
			"Destroy is a hard delete. DataHub asynchronously removes references to the form from " +
			"assigned entities, including completion and verification records. Structured property " +
			"values collected through the form's prompts are ordinary metadata on the target entities " +
			"and survive the form's deletion.\n\n" +
			"## URN\n\n" +
			"The URN is `urn:li:form:<form_id>` (deterministic, matching the DataHub Python SDK). If " +
			"`form_id` is omitted it is derived from `name`. ImportState accepts either the full URN " +
			"or a bare `form_id`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Terraform resource id; equal to `form_id`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN (`urn:li:form:<form_id>`).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"form_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Unique id (URN suffix). If omitted, derived from `name` as " +
					"`<sanitized-name>-<hash>`. Changing it forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-friendly name shown in the DataHub UI.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of what the form collects and why.",
			},
			"type": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("COMPLETION"),
				MarkdownDescription: "Form type: `COMPLETION` (collect metadata) or `VERIFICATION` " +
					"(attest that an asset complies). Defaults to `COMPLETION`.",
				Validators: []validator.String{enumString(formTypes...)},
			},
			"prompts": schema.ListNestedAttribute{
				Optional: true,
				MarkdownDescription: "Questions presented to the assigned actors. This resource owns " +
					"the complete list; prompts added outside Terraform are removed on the next apply.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Required: true,
							MarkdownDescription: "Prompt id, **globally unique across all forms**. Required here " +
								"(the server would otherwise mint a random UUID, which breaks deterministic state). " +
								"Prompt completion records are keyed by this id, so changing it resets responses.",
						},
						"title": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Question shown to the user.",
						},
						"description": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Optional help text for the prompt.",
						},
						"type": schema.StringAttribute{
							Required: true,
							MarkdownDescription: "Prompt type: `STRUCTURED_PROPERTY` (apply a structured property " +
								"to the entity) or `FIELDS_STRUCTURED_PROPERTY` (apply one to each schema field).",
							Validators: []validator.String{enumString(formPromptTypes...)},
						},
						"structured_property_urn": schema.StringAttribute{
							Required: true,
							MarkdownDescription: "URN of the structured property the prompt collects " +
								"(e.g. `datahub_structured_property.x.urn`). The property must exist when the form " +
								"is assigned; reference the resource rather than a raw URN so Terraform orders creation.",
						},
						"required": schema.BoolAttribute{
							Optional: true,
							Computed: true,
							Default:  booldefault.StaticBool(false),
							MarkdownDescription: "Whether the prompt must be answered for the form to complete. " +
								"Defaults to `false`. Field-level prompts (`FIELDS_STRUCTURED_PROPERTY`) cannot be required.",
						},
					},
				},
			},
			"actors": schema.SingleNestedAttribute{
				Optional: true,
				MarkdownDescription: "Who is asked to complete the form. Omitted, DataHub assigns the " +
					"form to the owners of each matched asset (`owners = true`). This resource owns the " +
					"complete `users` and `groups` lists.",
				Attributes: map[string]schema.Attribute{
					"owners": schema.BoolAttribute{
						Optional: true,
						Computed: true,
						Default:  booldefault.StaticBool(true),
						MarkdownDescription: "Assign the form to the owners of the assets it is applied to. " +
							"Defaults to `true`.",
					},
					"users": schema.ListAttribute{
						ElementType:         types.StringType,
						Optional:            true,
						MarkdownDescription: "Specific user URNs assigned the form (e.g. `[\"urn:li:corpuser:jdoe\"]`).",
					},
					"groups": schema.ListAttribute{
						ElementType:         types.StringType,
						Optional:            true,
						MarkdownDescription: "Specific group URNs assigned the form (e.g. `[\"urn:li:corpGroup:governance\"]`).",
					},
				},
			},
			"dynamic_assignment": schema.SingleNestedAttribute{
				Optional: true,
				MarkdownDescription: "Assigns the form automatically to every entity matching a search " +
					"filter, instead of assigning entities one by one. Omitting the block removes any " +
					"dynamic assignment from the form. Assignment runs asynchronously server-side; " +
					"removing or narrowing the filter does not retract the form from entities already " +
					"assigned.",
				Attributes: map[string]schema.Attribute{
					"or_filters": schema.ListNestedAttribute{
						Required: true,
						MarkdownDescription: "Disjunction (OR) of filter groups; each group is a conjunction " +
							"(AND) of facet predicates. An entity matches when it satisfies any group. " +
							"Commonly filtered fields: `_entityType`, `platform.keyword`, `domains.keyword`, " +
							"`container.keyword`, `typeNames.keyword` (sub-types), `owners.keyword`, " +
							"`tags.keyword`, `glossaryTerms.keyword`.",
						NestedObject: schema.NestedAttributeObject{
							Attributes: map[string]schema.Attribute{
								"and": schema.ListNestedAttribute{
									Required:            true,
									MarkdownDescription: "Conjunction (AND) of facet predicates; all must match.",
									NestedObject: schema.NestedAttributeObject{
										Attributes: map[string]schema.Attribute{
											"field": schema.StringAttribute{
												Required:            true,
												MarkdownDescription: "Search field to filter on (e.g. `platform.keyword`, `domains.keyword`).",
											},
											"values": schema.ListAttribute{
												ElementType:         types.StringType,
												Required:            true,
												MarkdownDescription: "Values to match (e.g. `[\"urn:li:dataPlatform:snowflake\"]`).",
											},
											"condition": schema.StringAttribute{
												Optional:            true,
												Computed:            true,
												Default:             stringdefault.StaticString("EQUAL"),
												MarkdownDescription: "Match operator. Defaults to `EQUAL`.",
												Validators:          []validator.String{enumString(filterOperators...)},
											},
											"negated": schema.BoolAttribute{
												Optional:            true,
												Computed:            true,
												Default:             booldefault.StaticBool(false),
												MarkdownDescription: "Invert the predicate (NOT). Defaults to `false`.",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// ValidateConfig rejects required field-level prompts, mirroring the DataHub
// Python SDK guard ("Schema field prompts cannot be marked as required").
// Unknown values are skipped: an unknown carries nothing to check, and
// refusing to validate one is what lets a variable-driven config through.
func (r *formResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg formResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if cfg.Prompts.IsNull() || cfg.Prompts.IsUnknown() {
		return
	}
	var prompts []formPromptModel
	resp.Diagnostics.Append(cfg.Prompts.ElementsAs(ctx, &prompts, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for i, p := range prompts {
		if p.Type.IsNull() || p.Type.IsUnknown() || p.Required.IsNull() || p.Required.IsUnknown() {
			continue
		}
		if p.Type.ValueString() == "FIELDS_STRUCTURED_PROPERTY" && p.Required.ValueBool() {
			resp.Diagnostics.AddAttributeError(
				path.Root("prompts").AtListIndex(i).AtName("required"),
				"Field-level prompts cannot be required",
				"DataHub does not support marking FIELDS_STRUCTURED_PROPERTY prompts as required; "+
					"set required = false or omit it.",
			)
		}
	}
}

// buildInput converts the plan model to a client input. existingID is empty on
// Create and the prior form_id on Update.
func (r *formResource) buildInput(ctx context.Context, plan formResourceModel, existingID string) (datahub.FormInput, diag.Diagnostics) {
	var diags diag.Diagnostics

	name := strings.TrimSpace(plan.Name.ValueString())
	if name == "" {
		diags.AddError("Invalid plan", "name is required")
		return datahub.FormInput{}, diags
	}

	formID := strings.TrimSpace(existingID)
	if formID == "" {
		formID = strings.TrimSpace(plan.FormID.ValueString())
	}
	if formID == "" {
		formID = uid.DeriveID(name, []byte(name), 48)
	}

	in := datahub.FormInput{
		ID:          formID,
		Name:        name,
		Description: strVal(plan.Description),
		Type:        plan.Type.ValueString(),
	}

	if !plan.Prompts.IsNull() && !plan.Prompts.IsUnknown() {
		var prompts []formPromptModel
		diags.Append(plan.Prompts.ElementsAs(ctx, &prompts, false)...)
		if diags.HasError() {
			return datahub.FormInput{}, diags
		}
		for _, p := range prompts {
			in.Prompts = append(in.Prompts, datahub.FormPrompt{
				ID:                    p.ID.ValueString(),
				Title:                 p.Title.ValueString(),
				Description:           strVal(p.Description),
				Type:                  p.Type.ValueString(),
				StructuredPropertyURN: p.StructuredPropertyURN.ValueString(),
				Required:              p.Required.ValueBool(),
			})
		}
	}

	if !plan.Actors.IsNull() && !plan.Actors.IsUnknown() {
		var actors formActorsModel
		diags.Append(plan.Actors.As(ctx, &actors, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return datahub.FormInput{}, diags
		}
		fa := &datahub.FormActors{Owners: actors.Owners.ValueBool()}
		diags.Append(actors.Users.ElementsAs(ctx, &fa.Users, false)...)
		diags.Append(actors.Groups.ElementsAs(ctx, &fa.Groups, false)...)
		if diags.HasError() {
			return datahub.FormInput{}, diags
		}
		in.Actors = fa
	}

	return in, diags
}

// assignmentFromModel extracts the or_filters from the dynamic_assignment
// object; nil when the block is absent.
func assignmentFromModel(ctx context.Context, o types.Object) ([]datahub.AndFilter, diag.Diagnostics) {
	var diags diag.Diagnostics
	if o.IsNull() || o.IsUnknown() {
		return nil, diags
	}
	var m formDynamicAssignmentModel
	diags.Append(o.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return nil, diags
	}
	orFilters, d := orFiltersFromModel(ctx, m.OrFilters)
	diags.Append(d...)
	return orFilters, diags
}

// writeDesiredState pushes the full desired state: the formInfo aspect, then
// the dynamicFormAssignment aspect (upserted when declared, cleared when not).
// Clearing also runs on create so a re-created URN cannot inherit a stale
// assignment from a previous life of the same form id.
func (r *formResource) writeDesiredState(ctx context.Context, plan formResourceModel, existingID string, diags *diag.Diagnostics) (datahub.FormInput, string) {
	in, d := r.buildInput(ctx, plan, existingID)
	diags.Append(d...)
	if diags.HasError() {
		return in, ""
	}

	orFilters, d := assignmentFromModel(ctx, plan.DynamicAssignment)
	diags.Append(d...)
	if diags.HasError() {
		return in, ""
	}

	urn, err := r.client.UpsertForm(ctx, in)
	if err != nil {
		diags.AddError("DataHub API Error", err.Error())
		return in, ""
	}

	if len(orFilters) > 0 {
		if err := r.client.UpsertDynamicFormAssignment(ctx, urn, orFilters); err != nil {
			diags.AddError("DataHub API Error",
				"form written but setting the dynamic assignment failed: "+err.Error())
			return in, ""
		}
	} else {
		if err := r.client.ClearDynamicFormAssignment(ctx, urn); err != nil {
			diags.AddError("DataHub API Error",
				"form written but clearing the dynamic assignment failed: "+err.Error())
			return in, ""
		}
	}
	return in, urn
}

func (r *formResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var plan formResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, urn := r.writeDesiredState(ctx, plan, "", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.FormID = types.StringValue(in.ID)
	plan.ID = types.StringValue(in.ID)
	plan.URN = types.StringValue(urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *formResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var plan, state formResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// form_id is RequiresReplace, so it is stable across an update; reuse it.
	in, urn := r.writeDesiredState(ctx, plan, state.FormID.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.FormID = types.StringValue(in.ID)
	plan.ID = types.StringValue(in.ID)
	plan.URN = types.StringValue(urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *formResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var state formResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := strings.TrimSpace(state.URN.ValueString())
	if urn == "" {
		formID := strings.TrimSpace(state.FormID.ValueString())
		if formID == "" {
			formID = strings.TrimSpace(state.ID.ValueString())
		}
		if formID == "" {
			resp.Diagnostics.AddError("Invalid state", "Missing urn/form_id/id in state; cannot read remote form.")
			return
		}
		urn = datahub.FormURNPrefix + formID
	}

	form, err := r.client.GetFormByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if form == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.FormID = types.StringValue(form.ID)
	state.ID = types.StringValue(form.ID)
	state.URN = types.StringValue(datahub.FormURNPrefix + form.ID)
	state.Name = types.StringValue(form.Name)
	state.Description = nullIfEmpty(form.Description)
	// The stored aspect omits type when it was never set; the schema default
	// demands COMPLETION in state to avoid a perpetual diff.
	if form.Type != "" {
		state.Type = types.StringValue(form.Type)
	} else {
		state.Type = types.StringValue("COMPLETION")
	}

	prompts, d := formPromptsToModel(ctx, form.Prompts, state.Prompts)
	resp.Diagnostics.Append(d...)
	state.Prompts = prompts

	actors, d := formActorsToModel(ctx, form.Actors, state.Actors)
	resp.Diagnostics.Append(d...)
	state.Actors = actors

	assignment, d := formAssignmentToModel(ctx, form.OrFilters)
	resp.Diagnostics.Append(d...)
	state.DynamicAssignment = assignment

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// formPromptsToModel converts read prompts to the framework list. A server-side
// empty list maps to null when the prior state was null (config omitted the
// attribute), so an omitted prompts list does not drift.
func formPromptsToModel(ctx context.Context, prompts []datahub.FormPrompt, prior types.List) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	if len(prompts) == 0 && prior.IsNull() {
		return types.ListNull(formPromptObjectType), diags
	}
	models := make([]formPromptModel, len(prompts))
	for i, p := range prompts {
		models[i] = formPromptModel{
			ID:                    types.StringValue(p.ID),
			Title:                 types.StringValue(p.Title),
			Description:           nullIfEmpty(p.Description),
			Type:                  types.StringValue(p.Type),
			StructuredPropertyURN: types.StringValue(p.StructuredPropertyURN),
			Required:              types.BoolValue(p.Required),
		}
	}
	l, d := types.ListValueFrom(ctx, formPromptObjectType, models)
	diags.Append(d...)
	return l, diags
}

// formActorsToModel converts the read actors to the framework object. The
// server always materialises actors (the aspect defaults to {owners: true}),
// so when the prior state was null and the server value is exactly that
// default, null is kept -- otherwise an omitted actors block would drift
// forever.
func formActorsToModel(ctx context.Context, actors *datahub.FormActors, prior types.Object) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	if actors == nil {
		return types.ObjectNull(formActorsObjectType.AttrTypes), diags
	}
	isDefault := actors.Owners && len(actors.Users) == 0 && len(actors.Groups) == 0
	if isDefault && prior.IsNull() {
		return types.ObjectNull(formActorsObjectType.AttrTypes), diags
	}
	m := formActorsModel{
		Owners: types.BoolValue(actors.Owners),
		Users:  types.ListNull(types.StringType),
		Groups: types.ListNull(types.StringType),
	}
	if len(actors.Users) > 0 {
		l, d := stringsToList(ctx, actors.Users)
		diags.Append(d...)
		m.Users = l
	}
	if len(actors.Groups) > 0 {
		l, d := stringsToList(ctx, actors.Groups)
		diags.Append(d...)
		m.Groups = l
	}
	o, d := types.ObjectValueFrom(ctx, formActorsObjectType.AttrTypes, m)
	diags.Append(d...)
	return o, diags
}

// formAssignmentToModel converts the read dynamicFormAssignment aspect to the
// framework object; null when the aspect is absent. Conditions the stored
// aspect omits read back as the schema default EQUAL.
func formAssignmentToModel(ctx context.Context, orFilters []datahub.AndFilter) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	if len(orFilters) == 0 {
		return types.ObjectNull(formDynamicAssignmentObjectType.AttrTypes), diags
	}
	normalised := make([]datahub.AndFilter, len(orFilters))
	for i, g := range orFilters {
		ng := datahub.AndFilter{And: make([]datahub.FacetFilter, len(g.And))}
		for j, f := range g.And {
			if f.Condition == "" {
				f.Condition = "EQUAL"
			}
			ng.And[j] = f
		}
		normalised[i] = ng
	}
	l, d := orFiltersToModel(ctx, normalised)
	diags.Append(d...)
	if diags.HasError() {
		return types.ObjectNull(formDynamicAssignmentObjectType.AttrTypes), diags
	}
	o, d := types.ObjectValueFrom(ctx, formDynamicAssignmentObjectType.AttrTypes,
		formDynamicAssignmentModel{OrFilters: l})
	diags.Append(d...)
	return o, diags
}

func (r *formResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var state formResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	urn := strings.TrimSpace(state.URN.ValueString())
	if urn == "" {
		resp.State.RemoveResource(ctx)
		return
	}
	if err := r.client.DeleteForm(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *formResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID",
			"Expected a DataHub form URN (e.g. urn:li:form:my-form) or a bare form_id.")
		return
	}
	formID := strings.TrimPrefix(raw, datahub.FormURNPrefix)
	if formID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Could not extract a form_id from the provided import ID.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), formID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("form_id"), formID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("urn"), datahub.FormURNPrefix+formID)...)
}
