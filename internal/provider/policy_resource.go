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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

const dataHubPolicyURNPrefix = "urn:li:dataHubPolicy:"

var (
	_ resource.Resource                   = &policyResource{}
	_ resource.ResourceWithConfigure      = &policyResource{}
	_ resource.ResourceWithImportState    = &policyResource{}
	_ resource.ResourceWithValidateConfig = &policyResource{}
)

type policyResource struct {
	client *datahub.Client
}

type policyResourceModel struct {
	ID          types.String `tfsdk:"id"`
	URN         types.String `tfsdk:"urn"`
	PolicyID    types.String `tfsdk:"policy_id"`
	Name        types.String `tfsdk:"name"`
	Type        types.String `tfsdk:"type"`
	State       types.String `tfsdk:"state"`
	Description types.String `tfsdk:"description"`
	Privileges  types.Set    `tfsdk:"privileges"`
	// Object rather than a Go pointer: a nested block fed from a variable or
	// for_each plans its Optional+Computed descendants as unknown, which a
	// pointer cannot hold. See the comment at the top of policy_model.go.
	Actors    types.Object `tfsdk:"actors"`
	Resources types.Object `tfsdk:"resources"`
}

type policyActorsModel struct {
	Users               types.Set  `tfsdk:"users"`
	Groups              types.Set  `tfsdk:"groups"`
	AllUsers            types.Bool `tfsdk:"all_users"`
	AllGroups           types.Bool `tfsdk:"all_groups"`
	ResourceOwners      types.Bool `tfsdk:"resource_owners"`
	ResourceOwnersTypes types.Set  `tfsdk:"resource_owners_types"`
}

type policyResourcesModel struct {
	Type         types.String `tfsdk:"type"`
	Resources    types.Set    `tfsdk:"resources"`
	AllResources types.Bool   `tfsdk:"all_resources"`
	Filter       types.Object `tfsdk:"filter"`
}

type policyMatchFilterModel struct {
	Criteria types.List `tfsdk:"criteria"`
}

type policyMatchCriterionModel struct {
	Field     types.String `tfsdk:"field"`
	Values    types.List   `tfsdk:"values"`
	Condition types.String `tfsdk:"condition"`
}

// policyMatchFields is the EntityFieldType enum accepted as a criterion field.
// The server rejects anything outside this set at aspect-write time via
// PolicyFieldTypeValidator, so validating here turns a failed apply into a plan
// error. RESOURCE_TYPE and RESOURCE_URN are deprecated aliases of TYPE and URN
// (resolved by the same field resolver providers); they are accepted so that a
// policy created before the rename still round-trips through import.
var policyMatchFields = []string{
	"TYPE", "URN", "OWNER", "DOMAIN", "GROUP_MEMBERSHIP",
	"DATA_PLATFORM_INSTANCE", "TAG", "CONTAINER", "GLOSSARY",
	"RESOURCE_TYPE", "RESOURCE_URN",
}

// policyMatchConditions is the PolicyMatchCondition enum.
var policyMatchConditions = []string{"EQUALS", "STARTS_WITH", "NOT_EQUALS"}

func NewPolicyResource() resource.Resource {
	return &policyResource{}
}

func (r *policyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	client := pd.Client
	r.client = client
}

func (r *policyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy"
}

func (r *policyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a DataHub access policy (`dataHubPolicy`).\n\n" +
			"Policies grant a set of privileges to a set of actors (users and/or groups), optionally " +
			"scoped to a set of resources. There are two policy types:\n\n" +
			"- `PLATFORM` -- top-level administrative privileges (e.g. `MANAGE_POLICIES`, " +
			"`MANAGE_INGESTION`). Omit the `resources` block.\n" +
			"- `METADATA` -- privileges over metadata entities (e.g. `EDIT_ENTITY_TAGS`), optionally " +
			"scoped via the `resources` block.\n\n" +
			"## Resource scoping\n\n" +
			"`resources` accepts two mutually exclusive forms:\n\n" +
			"- **`filter`** -- a list of criteria. The only form that can scope by tag, domain or " +
			"container, or across several entity types at once. This is what the DataHub UI writes.\n" +
			"- **`type` + `resources` + `all_resources`** -- a single entity type plus an explicit list " +
			"of resource URNs. The original form, since deprecated by DataHub. Still supported, but it " +
			"cannot express any of the scopes listed above.\n\n" +
			"Prefer `filter`. A criteria scope looks like this:\n\n" +
			"```terraform\n" +
			"resources = {\n" +
			"  filter = {\n" +
			"    criteria = [\n" +
			"      { field = \"TYPE\", values = [\"dataset\", \"container\"] },\n" +
			"      { field = \"TAG\", values = [\"urn:li:tag:pii\"] },\n" +
			"    ]\n" +
			"  }\n" +
			"}\n" +
			"```\n\n" +
			"Criteria combine with AND; the `values` within one criterion combine with OR. The example " +
			"above matches datasets and containers that also carry the `pii` tag. There is no top-level " +
			"OR -- a scope such as \"tagged `pii` OR in domain `finance`\" needs two policies.\n\n" +
			"The two forms cannot be combined: setting `filter` alongside `type`, `resources` or " +
			"`all_resources` is rejected at plan time, because DataHub would silently evaluate the " +
			"filter alone and ignore the rest.\n\n" +
			"## Naming\n\n" +
			"`policy_id` becomes the URN suffix (`urn:li:dataHubPolicy:<policy_id>`). Supplying an " +
			"explicit id keeps the URN deterministic and stable, avoiding the random UUID the DataHub " +
			"UI assigns.\n\n" +
			"## List ownership\n\n" +
			"This resource owns the complete `privileges`, `actors`, and `resources` sets and writes " +
			"the full desired state on every apply. Privileges or actors added outside Terraform are " +
			"removed on the next apply. These are modeled as sets, so element order is not significant.\n\n" +
			"## Privileges\n\n" +
			"`privileges` are free-form strings and are not validated by the provider, since the valid " +
			"set differs between DataHub releases and between OSS and DataHub Cloud. See the DataHub " +
			"`PoliciesConfig` for the authoritative list.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this policy (e.g., `urn:li:dataHubPolicy:my-policy`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"policy_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Unique identifier for the policy. Becomes the URN suffix (`urn:li:dataHubPolicy:<policy_id>`). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable display name for the policy.",
			},
			"type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Policy type: `PLATFORM` or `METADATA`.",
			},
			"state": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("ACTIVE"),
				MarkdownDescription: "Policy state: `ACTIVE` (enforced) or `INACTIVE`. Defaults to `ACTIVE`.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "Description of the policy's purpose.",
			},
			"privileges": schema.SetAttribute{
				Required:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Set of privilege strings the policy grants (e.g. `[\"MANAGE_POLICIES\"]`). Not validated by the provider.",
			},
			"actors": schema.SingleNestedAttribute{
				Required:            true,
				MarkdownDescription: "The actors the policy's privileges are granted to.",
				Attributes: map[string]schema.Attribute{
					"users": schema.SetAttribute{
						Optional:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "Set of user URNs (e.g. `[\"urn:li:corpuser:alice\"]`).",
					},
					"groups": schema.SetAttribute{
						Optional:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "Set of group URNs (e.g. `[\"urn:li:corpGroup:data-platform\"]`).",
					},
					"all_users": schema.BoolAttribute{
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(false),
						MarkdownDescription: "Apply the policy to all users. Defaults to `false`.",
					},
					"all_groups": schema.BoolAttribute{
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(false),
						MarkdownDescription: "Apply the policy to all groups. Defaults to `false`.",
					},
					"resource_owners": schema.BoolAttribute{
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(false),
						MarkdownDescription: "Apply the policy to owners of the targeted resource (METADATA policies only). Defaults to `false`.",
					},
					"resource_owners_types": schema.SetAttribute{
						Optional:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "Set of ownership-type URNs the resource_owners filter applies to (when `resource_owners = true`).",
					},
				},
			},
			"resources": schema.SingleNestedAttribute{
				Optional: true,
				MarkdownDescription: "Resource scope for METADATA policies. Omit for platform-wide PLATFORM policies.\n\n" +
					"Set either `filter` (criteria-based) or the deprecated `type` / `resources` / " +
					"`all_resources` trio, never both. Only `filter` can scope by tag, domain or container, " +
					"or across several entity types at once.",
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "The resource type the policy applies to (e.g. `dataset`). Deprecated by DataHub in favour of a `filter` criterion on `TYPE`; conflicts with `filter`.",
					},
					"resources": schema.SetAttribute{
						Optional:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "Set of specific resource URNs the policy applies to. Deprecated by DataHub in favour of a `filter` criterion on `URN`; conflicts with `filter`.",
					},
					"all_resources": schema.BoolAttribute{
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(false),
						MarkdownDescription: "Apply to all resources of `type`. Defaults to `false`. Deprecated by DataHub in favour of a `filter` with no `URN` criterion; conflicts with `filter`.",
					},
					"filter": schema.SingleNestedAttribute{
						Optional: true,
						MarkdownDescription: "Criteria-based resource scope. Mirrors the `resources.filter` form of the " +
							"`dataHubPolicyInfo` aspect, so a policy built in the DataHub UI can be transcribed " +
							"directly.\n\n" +
							"Mutually exclusive with `type`, `resources` and `all_resources`: when `filter` is " +
							"present DataHub evaluates it alone and ignores the legacy attributes entirely.",
						Attributes: map[string]schema.Attribute{
							"criteria": schema.ListNestedAttribute{
								Required: true,
								MarkdownDescription: "Conjunction (AND) of criteria -- every criterion must match for the policy to apply. " +
									"At least one criterion is required.\n\n" +
									"There is no top-level OR: a scope such as \"tagged `pii` OR in domain `finance`\" " +
									"needs two policies.",
								NestedObject: schema.NestedAttributeObject{
									Attributes: map[string]schema.Attribute{
										"field": schema.StringAttribute{
											Required: true,
											MarkdownDescription: "Entity field to match on. One of `TYPE`, `URN`, `OWNER`, `DOMAIN`, " +
												"`GROUP_MEMBERSHIP`, `DATA_PLATFORM_INSTANCE`, `TAG`, `CONTAINER`, `GLOSSARY`. " +
												"(`RESOURCE_TYPE` and `RESOURCE_URN` are accepted deprecated aliases of `TYPE` and `URN`.)\n\n" +
												"**Some fields match hierarchically and some do not**, which is not visible from the " +
												"configuration and materially changes how much a criterion covers:\n\n" +
												"- `DOMAIN`, `CONTAINER` and `GLOSSARY` match **descendants**. DataHub resolves an " +
												"entity's value set for these fields by expanding ancestors, so a criterion naming a " +
												"parent domain also matches entities in its child domains, one naming a database " +
												"container also matches datasets nested in its schemas, and one naming a glossary node " +
												"also matches entities tagged with terms beneath it.\n" +
												"- `TAG`, `TYPE`, `URN`, `OWNER`, `GROUP_MEMBERSHIP` and `DATA_PLATFORM_INSTANCE` " +
												"match **exactly**. Tags in particular carry no hierarchy here: a criterion matches only " +
												"entities carrying that precise tag URN.",
											Validators: []validator.String{enumString(policyMatchFields...)},
										},
										"values": schema.ListAttribute{
											Required:    true,
											ElementType: types.StringType,
											MarkdownDescription: "Values to match, disjunctively (OR) -- the criterion passes if any value matches. " +
												"Entity-typed fields take URNs (e.g. `[\"urn:li:tag:pii\"]`); `TYPE` takes entity " +
												"type names (e.g. `[\"dataset\", \"container\"]`).",
										},
										"condition": schema.StringAttribute{
											Optional:            true,
											Computed:            true,
											Default:             stringdefault.StaticString("EQUALS"),
											MarkdownDescription: "Match condition: `EQUALS`, `STARTS_WITH` or `NOT_EQUALS`. Defaults to `EQUALS`.",
											Validators:          []validator.String{enumString(policyMatchConditions...)},
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

// ValidateConfig rejects a resources block that mixes the criteria filter with
// the deprecated legacy attributes. The server accepts both without complaint
// and then silently evaluates only the filter (PolicyEngine.getFilter returns
// early when it is set), so a policy configured with both would appear to be
// scoped more narrowly than it actually is. Failing at plan time is the only
// place a user finds out.
func (r *policyResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg policyResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Anything still unknown at validate time cannot be checked: the value is
	// only resolved during plan. Skipping is correct rather than lenient - the
	// conflicts below are re-derivable from the plan, and refusing to validate
	// an unknown is what lets a variable-driven or for_each-driven block through
	// at all.
	res, ok, d := policyResourcesFromObject(ctx, cfg.Resources)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() || !ok || res.Filter.IsNull() || res.Filter.IsUnknown() {
		return
	}

	var filter policyMatchFilterModel
	resp.Diagnostics.Append(res.Filter.As(ctx, &filter, objectAsOptions)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resourcesPath := path.Root("resources")
	const conflictSummary = "Conflicting resource scope"
	const conflictDetail = "%s cannot be combined with resources.filter. DataHub evaluates the " +
		"filter alone and ignores the legacy attributes, so the policy would not be scoped as " +
		"written. Express the same scope as a %s criterion instead."

	if configSupplies(res.Type) {
		resp.Diagnostics.AddAttributeError(resourcesPath.AtName("type"), conflictSummary,
			fmt.Sprintf(conflictDetail, "resources.type", "TYPE"))
	}
	if configSuppliesSet(res.Resources) {
		resp.Diagnostics.AddAttributeError(resourcesPath.AtName("resources"), conflictSummary,
			fmt.Sprintf(conflictDetail, "resources.resources", "URN"))
	}
	if configSuppliesTrueBool(res.AllResources) {
		resp.Diagnostics.AddAttributeError(resourcesPath.AtName("all_resources"), conflictSummary,
			"resources.all_resources cannot be combined with resources.filter. A filter already "+
				"applies to every resource matching its criteria; drop all_resources.")
	}

	criteriaPath := resourcesPath.AtName("filter").AtName("criteria")
	criteria, ok, d := policyCriteriaFromList(ctx, filter.Criteria)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() || !ok {
		return
	}

	if len(criteria) == 0 {
		resp.Diagnostics.AddAttributeError(criteriaPath,
			"Empty criteria list",
			"resources.filter.criteria must contain at least one criterion. An empty filter matches "+
				"every resource of every type, which is rarely intended -- omit the resources block "+
				"entirely if the policy really is unscoped.")
	}

	for i, c := range criteria {
		if c.Values.IsNull() || c.Values.IsUnknown() {
			continue
		}
		if len(c.Values.Elements()) == 0 {
			resp.Diagnostics.AddAttributeError(criteriaPath.AtListIndex(i).AtName("values"),
				"Empty values list",
				"values must contain at least one value. A criterion with no values never matches "+
					"any resource, so the policy would silently grant nothing.")
		}
	}
}

func (r *policyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan policyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := dataHubPolicyURNPrefix + plan.PolicyID.ValueString()
	in, diags := policyInputFromModel(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := r.client.UpsertPolicy(ctx, urn, in); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = types.StringValue(urn)
	plan.URN = types.StringValue(urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *policyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state policyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}

	policy, err := r.client.GetPolicyByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if policy == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	diags := applyPolicyToModel(ctx, policy, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *policyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan, state policyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	in, diags := policyInputFromModel(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := r.client.UpsertPolicy(ctx, urn, in); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = state.ID
	plan.URN = state.URN
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *policyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state policyResourceModel
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

	if err := r.client.DeletePolicy(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *policyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub policy URN (e.g., urn:li:dataHubPolicy:my-policy) or a bare policy ID.")
		return
	}

	var policyID, urn string
	if strings.HasPrefix(raw, dataHubPolicyURNPrefix) {
		urn = raw
		policyID = strings.TrimPrefix(raw, dataHubPolicyURNPrefix)
	} else {
		policyID = raw
		urn = dataHubPolicyURNPrefix + policyID
	}
	if policyID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub policy URN (e.g., urn:li:dataHubPolicy:my-policy) or a bare policy ID.")
		return
	}

	policy, err := r.client.GetPolicyByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if policy == nil {
		resp.Diagnostics.AddError(
			"Policy not found",
			fmt.Sprintf("No policy with URN %q was found in DataHub. Verify the ID or URN and retry.", urn),
		)
		return
	}

	state := policyResourceModel{
		PolicyID: types.StringValue(policy.ID),
	}
	diags := applyPolicyToModel(ctx, policy, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
