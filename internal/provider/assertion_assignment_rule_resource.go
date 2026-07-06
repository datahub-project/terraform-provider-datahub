// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
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
	_ resource.Resource                = &assertionAssignmentRuleResource{}
	_ resource.ResourceWithConfigure   = &assertionAssignmentRuleResource{}
	_ resource.ResourceWithImportState = &assertionAssignmentRuleResource{}
)

// filterOperators is the FilterOperator enum accepted by facet conditions.
var filterOperators = []string{
	"CONTAIN", "EQUAL", "IEQUAL", "IN", "EXISTS", "GREATER_THAN",
	"GREATER_THAN_OR_EQUAL_TO", "LESS_THAN", "LESS_THAN_OR_EQUAL_TO",
	"BETWEEN", "START_WITH", "END_WITH", "DESCENDANTS_INCL",
	"ANCESTORS_INCL", "RELATED_INCL",
}

// assertionActionTypes is the AssertionActionType enum for success/failure actions.
var assertionActionTypes = []string{"RAISE_INCIDENT", "RESOLVE_INCIDENT"}

type assertionAssignmentRuleResource struct {
	client *datahub.Client
}

type assertionAssignmentRuleResourceModel struct {
	ID        types.String `tfsdk:"id"`
	URN       types.String `tfsdk:"urn"`
	RuleID    types.String `tfsdk:"rule_id"`
	Name      types.String `tfsdk:"name"`
	Mode      types.String `tfsdk:"mode"`
	Query     types.String `tfsdk:"query"`
	OrFilters types.List   `tfsdk:"or_filters"`
	Freshness types.Object `tfsdk:"freshness"`
	Volume    types.Object `tfsdk:"volume"`
}

type andGroupModel struct {
	And types.List `tfsdk:"and"`
}

type facetFilterModel struct {
	Field     types.String `tfsdk:"field"`
	Values    types.List   `tfsdk:"values"`
	Condition types.String `tfsdk:"condition"`
	Negated   types.Bool   `tfsdk:"negated"`
}

type categoryConfigModel struct {
	SourceType       types.String `tfsdk:"source_type"`
	OnSuccessActions types.List   `tfsdk:"on_success_actions"`
	OnFailureActions types.List   `tfsdk:"on_failure_actions"`
}

var facetFilterObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"field":     types.StringType,
	"values":    types.ListType{ElemType: types.StringType},
	"condition": types.StringType,
	"negated":   types.BoolType,
}}

var andGroupObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"and": types.ListType{ElemType: facetFilterObjectType},
}}

var categoryConfigObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"source_type":        types.StringType,
	"on_success_actions": types.ListType{ElemType: types.StringType},
	"on_failure_actions": types.ListType{ElemType: types.StringType},
}}

// NewAssertionAssignmentRuleResource returns a new datahub_assertion_assignment_rule resource.
func NewAssertionAssignmentRuleResource() resource.Resource {
	return &assertionAssignmentRuleResource{}
}

func (r *assertionAssignmentRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *assertionAssignmentRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_assertion_assignment_rule"
}

func categoryConfigSchema(category string) schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Optional: true,
		MarkdownDescription: fmt.Sprintf(
			"Enables auto-assignment of a **%s** monitor to every matching dataset. "+
				"Omit the block to leave %s monitoring unmanaged by this rule.", category, category),
		Attributes: map[string]schema.Attribute{
			"source_type": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Preferred evaluation source for the created monitors " +
					"(e.g. `INFORMATION_SCHEMA`, `AUDIT_LOG`, `DATAHUB_OPERATION`). Platform-specific; not enum-validated.",
			},
			"on_success_actions": schema.ListAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Actions taken when a created assertion passes (e.g. `[\"RESOLVE_INCIDENT\"]`).",
				Validators: []validator.List{
					enumList(assertionActionTypes...),
				},
			},
			"on_failure_actions": schema.ListAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Actions taken when a created assertion fails (e.g. `[\"RAISE_INCIDENT\"]`).",
				Validators: []validator.List{
					enumList(assertionActionTypes...),
				},
			},
		},
	}
}

func (r *assertionAssignmentRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: cloudOnlyBadge +
			"Creates and manages a DataHub Cloud **assertion assignment rule** -- a declarative rule that " +
			"auto-assigns freshness and/or volume monitors to every dataset matching a search filter. " +
			"One rule replaces hand-authoring a per-dataset assertion across many datasets: as new datasets " +
			"match the filter, they are monitored automatically.\n\n" +
			"Assertion assignment rules are a DataHub Cloud capability owned by platform/observability teams. " +
			"DataHub Cloud upgrades on its own release cadence, so a release may occasionally affect this " +
			"resource; pin the provider version for client-side stability and open an issue if you hit a problem.\n\n" +
			"Only **freshness** and **volume** monitors are auto-assignable by a rule; sql/field/schema " +
			"assertions must be authored per-dataset with the typed assertion resources.\n\n" +
			"## Filter semantics\n\n" +
			"`or_filters` is a disjunction (OR) of filter groups; each group is a conjunction (AND) of facet " +
			"predicates. A dataset matches the rule when it satisfies **any** group. This mirrors DataHub's " +
			"search filter model. This resource owns the complete filter and config -- edits made outside " +
			"Terraform are overwritten on the next apply.\n\n" +
			"## URN\n\n" +
			"The URN is `urn:li:assertionAssignmentRule:<rule_id>` (deterministic). If `rule_id` is omitted it " +
			"is derived from `name`. ImportState accepts either the full URN or a bare `rule_id`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Terraform resource id; equal to `rule_id`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN (`urn:li:assertionAssignmentRule:<rule_id>`).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"rule_id": schema.StringAttribute{
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
			"mode": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("ENABLED"),
				MarkdownDescription: "Rule mode: `ENABLED` (assignments active) or `DISABLED` (rule retained but inactive). Defaults to `ENABLED`.",
				Validators:          []validator.String{enumString("ENABLED", "DISABLED")},
			},
			"query": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("*"),
				MarkdownDescription: "Free-text search query paired with `or_filters`. Defaults to `*` (match all, filtered by `or_filters`).",
			},
			"or_filters": schema.ListNestedAttribute{
				Required: true,
				MarkdownDescription: "Disjunction (OR) of filter groups. A dataset matches when it satisfies any group. " +
					"At least one group is required.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"and": schema.ListNestedAttribute{
							Required:            true,
							MarkdownDescription: "Conjunction (AND) of facet predicates; all must match.",
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"field": schema.StringAttribute{
										Required:            true,
										MarkdownDescription: "Search field to filter on (e.g. `platform`, `domains`, `tags`, `container`).",
									},
									"values": schema.ListAttribute{
										ElementType:         types.StringType,
										Required:            true,
										MarkdownDescription: "Values to match (e.g. `[\"urn:li:dataPlatform:postgres\"]`).",
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
			"freshness": categoryConfigSchema("freshness"),
			"volume":    categoryConfigSchema("volume"),
		},
	}
}

// buildInput converts the plan model to a client input. existingID is empty on
// Create and the prior rule_id on Update.
func (r *assertionAssignmentRuleResource) buildInput(ctx context.Context, plan assertionAssignmentRuleResourceModel, existingID string) (datahub.AssertionAssignmentRuleInput, diag.Diagnostics) {
	var diags diag.Diagnostics

	name := strings.TrimSpace(plan.Name.ValueString())
	if name == "" {
		diags.AddError("Invalid plan", "name is required")
		return datahub.AssertionAssignmentRuleInput{}, diags
	}

	ruleID := strings.TrimSpace(existingID)
	if ruleID == "" {
		ruleID = strings.TrimSpace(plan.RuleID.ValueString())
	}
	if ruleID == "" {
		ruleID = uid.DeriveID(name, []byte(name), 48)
	}

	orFilters, d := orFiltersFromModel(ctx, plan.OrFilters)
	diags.Append(d...)

	freshness, d := categoryConfigFromModel(ctx, plan.Freshness)
	diags.Append(d...)
	volume, d := categoryConfigFromModel(ctx, plan.Volume)
	diags.Append(d...)

	if diags.HasError() {
		return datahub.AssertionAssignmentRuleInput{}, diags
	}

	return datahub.AssertionAssignmentRuleInput{
		ID:        ruleID,
		Name:      name,
		Query:     plan.Query.ValueString(),
		OrFilters: orFilters,
		Mode:      plan.Mode.ValueString(),
		Freshness: freshness,
		Volume:    volume,
	}, diags
}

func orFiltersFromModel(ctx context.Context, l types.List) ([]datahub.AndFilter, diag.Diagnostics) {
	var groups []andGroupModel
	diags := l.ElementsAs(ctx, &groups, false)
	if diags.HasError() {
		return nil, diags
	}
	out := make([]datahub.AndFilter, 0, len(groups))
	for _, g := range groups {
		var facets []facetFilterModel
		diags.Append(g.And.ElementsAs(ctx, &facets, false)...)
		if diags.HasError() {
			return nil, diags
		}
		af := datahub.AndFilter{}
		for _, f := range facets {
			var values []string
			diags.Append(f.Values.ElementsAs(ctx, &values, false)...)
			af.And = append(af.And, datahub.FacetFilter{
				Field:     f.Field.ValueString(),
				Values:    values,
				Condition: f.Condition.ValueString(),
				Negated:   f.Negated.ValueBool(),
			})
		}
		out = append(out, af)
	}
	return out, diags
}

func categoryConfigFromModel(ctx context.Context, o types.Object) (*datahub.AssertionAssignmentRuleCategoryConfig, diag.Diagnostics) {
	var diags diag.Diagnostics
	if o.IsNull() || o.IsUnknown() {
		return nil, diags
	}
	var m categoryConfigModel
	diags.Append(o.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return nil, diags
	}
	cfg := &datahub.AssertionAssignmentRuleCategoryConfig{SourceType: strVal(m.SourceType)}
	diags.Append(m.OnSuccessActions.ElementsAs(ctx, &cfg.OnSuccessActions, false)...)
	diags.Append(m.OnFailureActions.ElementsAs(ctx, &cfg.OnFailureActions, false)...)
	return cfg, diags
}

func (r *assertionAssignmentRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var plan assertionAssignmentRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, d := r.buildInput(ctx, plan, "")
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn, err := r.client.CreateAssertionAssignmentRule(ctx, in)
	if err != nil {
		if errors.Is(err, datahub.ErrAssertionAssignmentRuleCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required",
				"datahub_assertion_assignment_rule requires DataHub Cloud. "+
					"The configured DataHub instance does not expose assertion assignment rule management.")
			return
		}
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	// createAssertionAssignmentRule always starts a rule ENABLED; apply a
	// DISABLED mode via a follow-up update (mode is not a create input).
	if in.Mode == "DISABLED" {
		if err := r.client.UpdateAssertionAssignmentRule(ctx, urn, in); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", fmt.Sprintf("rule created but setting mode failed: %s", err.Error()))
			return
		}
	}

	r.applyComputed(&plan, in, urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *assertionAssignmentRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var plan, state assertionAssignmentRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// rule_id is RequiresReplace, so it is stable across an update; reuse it.
	in, d := r.buildInput(ctx, plan, state.RuleID.ValueString())
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = datahub.AssertionAssignmentRuleURNPrefix + in.ID
	}
	if err := r.client.UpdateAssertionAssignmentRule(ctx, urn, in); err != nil {
		if errors.Is(err, datahub.ErrAssertionAssignmentRuleCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required", "datahub_assertion_assignment_rule requires DataHub Cloud.")
			return
		}
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	r.applyComputed(&plan, in, urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// applyComputed fills the computed identity fields after a successful write.
func (r *assertionAssignmentRuleResource) applyComputed(plan *assertionAssignmentRuleResourceModel, in datahub.AssertionAssignmentRuleInput, urn string) {
	plan.RuleID = types.StringValue(in.ID)
	plan.ID = types.StringValue(in.ID)
	plan.URN = types.StringValue(urn)
}

func (r *assertionAssignmentRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var state assertionAssignmentRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := strings.TrimSpace(state.URN.ValueString())
	if urn == "" {
		ruleID := strings.TrimSpace(state.RuleID.ValueString())
		if ruleID == "" {
			ruleID = strings.TrimSpace(state.ID.ValueString())
		}
		if ruleID == "" {
			resp.Diagnostics.AddError("Invalid state", "Missing urn/rule_id/id in state; cannot read remote assignment rule.")
			return
		}
		urn = datahub.AssertionAssignmentRuleURNPrefix + ruleID
	}

	info, err := r.client.GetAssertionAssignmentRuleByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if info == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.RuleID = types.StringValue(info.ID)
	state.ID = types.StringValue(info.ID)
	state.URN = types.StringValue(datahub.AssertionAssignmentRuleURNPrefix + info.ID)
	if info.Name != "" {
		state.Name = types.StringValue(info.Name)
	}
	if info.Mode != "" {
		state.Mode = types.StringValue(info.Mode)
	}
	state.Query = types.StringValue(info.Query)

	orFilters, d := orFiltersToModel(ctx, info.OrFilters)
	resp.Diagnostics.Append(d...)
	state.OrFilters = orFilters

	freshness, d := categoryConfigToModel(ctx, info.Freshness)
	resp.Diagnostics.Append(d...)
	state.Freshness = freshness
	volume, d := categoryConfigToModel(ctx, info.Volume)
	resp.Diagnostics.Append(d...)
	state.Volume = volume

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func orFiltersToModel(ctx context.Context, orFilters []datahub.AndFilter) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	groups := make([]andGroupModel, len(orFilters))
	for i, g := range orFilters {
		facets := make([]facetFilterModel, len(g.And))
		for j, f := range g.And {
			values, d := stringsToList(ctx, f.Values)
			diags.Append(d...)
			facets[j] = facetFilterModel{
				Field:     types.StringValue(f.Field),
				Values:    values,
				Condition: types.StringValue(f.Condition),
				Negated:   types.BoolValue(f.Negated),
			}
		}
		fl, d := types.ListValueFrom(ctx, facetFilterObjectType, facets)
		diags.Append(d...)
		groups[i] = andGroupModel{And: fl}
	}
	l, d := types.ListValueFrom(ctx, andGroupObjectType, groups)
	diags.Append(d...)
	return l, diags
}

func categoryConfigToModel(ctx context.Context, cfg *datahub.AssertionAssignmentRuleCategoryConfig) (types.Object, diag.Diagnostics) {
	if cfg == nil {
		return types.ObjectNull(categoryConfigObjectType.AttrTypes), nil
	}
	var diags diag.Diagnostics
	onSuccess, d := stringsToList(ctx, cfg.OnSuccessActions)
	diags.Append(d...)
	onFailure, d := stringsToList(ctx, cfg.OnFailureActions)
	diags.Append(d...)
	m := categoryConfigModel{
		SourceType:       nullIfEmpty(cfg.SourceType),
		OnSuccessActions: onSuccess,
		OnFailureActions: onFailure,
	}
	o, d := types.ObjectValueFrom(ctx, categoryConfigObjectType.AttrTypes, m)
	diags.Append(d...)
	return o, diags
}

func (r *assertionAssignmentRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var state assertionAssignmentRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	urn := strings.TrimSpace(state.URN.ValueString())
	if urn == "" {
		resp.State.RemoveResource(ctx)
		return
	}
	if err := r.client.DeleteAssertionAssignmentRule(ctx, urn); err != nil {
		if errors.Is(err, datahub.ErrAssertionAssignmentRuleCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required", "datahub_assertion_assignment_rule requires DataHub Cloud.")
			return
		}
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *assertionAssignmentRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID",
			"Expected a DataHub assertion assignment rule URN (e.g. urn:li:assertionAssignmentRule:my-rule) or a bare rule_id.")
		return
	}
	ruleID := strings.TrimPrefix(raw, datahub.AssertionAssignmentRuleURNPrefix)
	if ruleID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Could not extract a rule_id from the provided import ID.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), ruleID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("rule_id"), ruleID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("urn"), datahub.AssertionAssignmentRuleURNPrefix+ruleID)...)
}
