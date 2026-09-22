// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ resource.Resource                = &entityOwnershipResource{}
	_ resource.ResourceWithConfigure   = &entityOwnershipResource{}
	_ resource.ResourceWithImportState = &entityOwnershipResource{}
)

// supportedOwnershipTargetValidator rejects an entity_urn whose entity type the
// provider does not accept as an owner assignment target.
type supportedOwnershipTargetValidator struct{}

func (v supportedOwnershipTargetValidator) Description(_ context.Context) string {
	return "must be the URN of a supported target entity type: " + strings.Join(datahub.SupportedOwnershipEntityTypes(), ", ")
}

func (v supportedOwnershipTargetValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v supportedOwnershipTargetValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	// Two failure modes this guard exists to pre-empt, neither of which the
	// server reports usefully. An entity type that does not declare the
	// `ownership` aspect fails at ingestion with "Unknown aspect ownership for
	// entity <name>", wrapped by the resolver into a generic "Failed to batch
	// add Owners". And a data asset would be accepted happily, which is worse:
	// the provider's charter excludes per-asset enrichment precisely because
	// apply would then overwrite what business users curate by hand.
	if _, _, err := datahub.OwnershipTargetType(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Unsupported owner assignment target", err.Error())
	}
}

// ownerPrincipalURNValidator rejects an owner_urn that is neither a corp user
// nor a corp group. DataHub's OwnerEntityType enum has exactly those two
// members, and the provider derives the value from the URN rather than asking
// for it, so an unrecognised owner URN kind has no derivable answer.
type ownerPrincipalURNValidator struct{}

func (v ownerPrincipalURNValidator) Description(_ context.Context) string {
	return "must be a corp user URN (urn:li:corpuser:...) or a corp group URN (urn:li:corpGroup:...)"
}

func (v ownerPrincipalURNValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v ownerPrincipalURNValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := datahub.OwnerEntityTypeFor(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid owner URN", err.Error())
	}
}

// ownershipTypeURNValidator rejects an ownership_type_urn that is not an
// ownership type URN.
type ownershipTypeURNValidator struct{}

func (v ownershipTypeURNValidator) Description(_ context.Context) string {
	return fmt.Sprintf("must be an ownership type URN starting with %q", datahub.OwnershipTypeURNPrefix)
}

func (v ownershipTypeURNValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v ownershipTypeURNValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	got := req.ConfigValue.ValueString()
	if !strings.HasPrefix(got, datahub.OwnershipTypeURNPrefix) || got == datahub.OwnershipTypeURNPrefix {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid ownership type URN",
			fmt.Sprintf("%q is not an ownership type URN; expected a URN starting with %q. "+
				"Use datahub_ownership_type.<name>.urn for a custom type, or one of DataHub's built-in "+
				"types (%s__system__technical_owner, %s__system__business_owner, "+
				"%s__system__data_steward, %s__system__none).",
				got, datahub.OwnershipTypeURNPrefix,
				datahub.OwnershipTypeURNPrefix, datahub.OwnershipTypeURNPrefix,
				datahub.OwnershipTypeURNPrefix, datahub.OwnershipTypeURNPrefix),
		)
	}
}

type entityOwnershipResource struct {
	client *datahub.Client
}

// entityOwnershipResourceModel is the resource's state shape.
//
// Owner is types.Set rather than []ownerModel, and that is not stylistic: a Go
// slice has no representation for an unknown value, so a block fed from a
// variable or a for_each would fail config conversion before the provider ran.
// See "Unknown Values in Nested Attributes" in
// docs/design/datahub-model-and-resource-design.md -- this class of bug shipped
// twice in this provider before the rule was written down.
type entityOwnershipResourceModel struct {
	ID        types.String `tfsdk:"id"`
	EntityURN types.String `tfsdk:"entity_urn"`
	Owner     types.Set    `tfsdk:"owner"`
}

// ownerModel is the conversion target for one element of the owner set. It is
// only ever populated from resolved values (in Create, Update and Delete), never
// used as a model field, which is what keeps the unknown-value hazard away.
type ownerModel struct {
	OwnerURN         types.String `tfsdk:"owner_urn"`
	OwnershipTypeURN types.String `tfsdk:"ownership_type_urn"`
}

// ownerObjectType is the element type of the owner set, needed to build a
// null/empty set and to convert edges back into state.
func ownerObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"owner_urn":          types.StringType,
		"ownership_type_urn": types.StringType,
	}}
}

func NewEntityOwnershipResource() resource.Resource {
	return &entityOwnershipResource{}
}

func (r *entityOwnershipResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	r.client = pd.Client
}

func (r *entityOwnershipResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_entity_ownership"
}

func (r *entityOwnershipResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Assigns owners to a DataHub entity. One resource per entity, holding a set of " +
			"`owner` entries; each entry is one `(owner, ownership type)` pair on the entity's " +
			"`ownership` aspect.\n\n" +
			"`(entity, ownership type)` is **not** a unique key, and both ways of repeating are " +
			"ordinary. Several owners may share one ownership type -- a role is usually held by " +
			"more than one party -- and one owner may hold several ownership types. Each pair is " +
			"its own entry and its own edge in DataHub. The only entry that cannot be repeated is " +
			"an exact duplicate of another (same `owner_urn` *and* same `ownership_type_urn`): " +
			"`owner` is a set, so Terraform collapses those two entries into one before the " +
			"provider sees them, which matches what DataHub stores.\n\n" +
			"## Owners added outside Terraform are preserved\n\n" +
			"This resource **merges**: it owns only the pairs it declares. Create adds them, " +
			"update adds newly declared pairs and removes only pairs it previously declared, and " +
			"destroy removes only pairs it declared. An owner added in the DataHub UI survives " +
			"create, update *and* destroy untouched.\n\n" +
			"This is a deliberate departure from the provider's usual rule that a resource owns " +
			"the complete list of any aspect it writes. The reason is the primary use case: " +
			"adopting a glossary, domain tree or data product catalogue that people have already " +
			"been curating by hand. Owning the whole `ownership` aspect would silently delete " +
			"every hand-assigned owner on the first apply. `datahub_structured_property_assignment` " +
			"merges per property for the same reason, and the provider's " +
			"`defaults.structured_properties` is a per-property latch rather than a whole-aspect " +
			"write.\n\n" +
			"The corollary is the usual one for merge semantics: removing an `owner` entry from " +
			"the configuration *does* remove that pair from DataHub on the next apply. What is " +
			"preserved is what Terraform never declared, not everything that happens to be there.\n\n" +
			"## Supported target entity types\n\n" +
			"`domain`, `glossaryTerm`, `glossaryNode`, `dataProduct`, `corpGroup`, `tag`, `form` " +
			"and `dataHubIngestionSource` -- the platform-configuration entities that both carry " +
			"the `ownership` aspect and fall inside this provider's remit.\n\n" +
			"Everything else is rejected at plan time, for one of two reasons:\n\n" +
			"- **Ingested data assets** (`dataset`, `chart`, `dashboard`, `dataJob`, ...) carry " +
			"`ownership`, but their metadata belongs to ingestion and to business users editing " +
			"the catalog. Managing it here would mean every apply overwrote their edits.\n" +
			"- **`corpuser`, `dataContract`, `dataHubPolicy` and `structuredProperty`** do not " +
			"declare the `ownership` aspect at all, so a write would have nowhere to land. Note " +
			"`corpuser` and `dataContract` *are* valid `datahub_structured_property_assignment` " +
			"targets; the two allowlists differ, and this one is the narrower.\n\n" +
			"## Import\n\n" +
			"Import by entity URN. Import adopts **every** owner then present on the entity, so " +
			"the imported resource declares them all; drop an entry from the configuration " +
			"afterwards and the next apply removes that pair from DataHub.\n\n" +
			"## References\n\n" +
			"Prefer expression inputs so Terraform creates things in the right order and you never " +
			"hand-assemble a URN: set `entity_urn` to the target's `.urn` (e.g. " +
			"`datahub_glossary_term.<name>.urn`), `ownership_type_urn` to " +
			"`datahub_ownership_type.<name>.urn`, and `owner_urn` to " +
			"`datahub_corp_group.<name>.urn` or `datahub_corp_user.<name>.urn`. Raw URN strings " +
			"work for principals and types managed outside Terraform -- DataHub validates that " +
			"each one exists and rejects the whole write otherwise -- but then you are responsible " +
			"for the ordering.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The target entity's URN. There is one `datahub_entity_ownership` resource per entity, so the entity URN is the resource identifier.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"entity_urn": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "URN of the entity to assign owners to. Must be a `domain`, `glossaryTerm`, `glossaryNode`, `dataProduct`, `corpGroup`, `tag`, `form` or `dataHubIngestionSource` URN. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					supportedOwnershipTargetValidator{},
				},
			},
			"owner": schema.SetNestedAttribute{
				Required: true,
				MarkdownDescription: "The set of `(owner, ownership type)` pairs this resource manages on the entity. " +
					"Unordered: reordering the entries produces no diff. Two entries may share an " +
					"`owner_urn`, or share an `ownership_type_urn`, or both differ -- only an exact " +
					"duplicate of both attributes collapses, since this is a set.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"owner_urn": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "URN of the owning principal: a corp user (`urn:li:corpuser:<username>`, including service accounts) or a corp group (`urn:li:corpGroup:<group_id>`). DataHub's `OwnerEntityType` is derived from this URN, so it never has to be restated. The principal must already exist -- DataHub resolves every owner URN and rejects the whole write with `Owner with urn ... does not exist.` otherwise.",
							Validators: []validator.String{
								ownerPrincipalURNValidator{},
							},
						},
						"ownership_type_urn": schema.StringAttribute{
							Required: true,
							MarkdownDescription: "URN of the ownership type this owner holds, e.g. `datahub_ownership_type.<name>.urn`. DataHub's built-in types are addressable as ownership type entities too: " +
								"`urn:li:ownershipType:__system__technical_owner`, `urn:li:ownershipType:__system__business_owner`, `urn:li:ownershipType:__system__data_steward` and `urn:li:ownershipType:__system__none`. " +
								"Required rather than optional on purpose: the pair is what this resource owns, and a removal that omitted the type would take the owner's *other* ownership types with it.",
							Validators: []validator.String{
								ownershipTypeURNValidator{},
							},
						},
					},
				},
			},
		},
	}
}

// ownerEdgesFromSet converts a resolved owner set into client edges. Callers
// must only pass a known, non-null set (Create, Update and Delete all have one).
func ownerEdgesFromSet(ctx context.Context, set types.Set) ([]datahub.OwnerEdge, diag.Diagnostics) {
	var diags diag.Diagnostics
	if set.IsNull() || set.IsUnknown() {
		return nil, diags
	}
	var models []ownerModel
	diags.Append(set.ElementsAs(ctx, &models, false)...)
	if diags.HasError() {
		return nil, diags
	}
	edges := make([]datahub.OwnerEdge, 0, len(models))
	for _, m := range models {
		edges = append(edges, datahub.OwnerEdge{
			OwnerURN:         m.OwnerURN.ValueString(),
			OwnershipTypeURN: m.OwnershipTypeURN.ValueString(),
		})
	}
	return edges, diags
}

// ownerSetFromEdges converts client edges back into the state set.
func ownerSetFromEdges(ctx context.Context, edges []datahub.OwnerEdge) (types.Set, diag.Diagnostics) {
	models := make([]ownerModel, 0, len(edges))
	for _, e := range edges {
		models = append(models, ownerModel{
			OwnerURN:         types.StringValue(e.OwnerURN),
			OwnershipTypeURN: types.StringValue(e.OwnershipTypeURN),
		})
	}
	return types.SetValueFrom(ctx, ownerObjectType(), models)
}

// formatOwnerEdges renders edges for a diagnostic, sorted so the message is
// stable regardless of set iteration order.
func formatOwnerEdges(edges []datahub.OwnerEdge) string {
	parts := make([]string, 0, len(edges))
	for _, e := range edges {
		parts = append(parts, fmt.Sprintf("(%s, %s)", e.OwnerURN, e.OwnershipTypeURN))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func (r *entityOwnershipResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan entityOwnershipResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	entityURN := plan.EntityURN.ValueString()
	desired, diags := ownerEdgesFromSet(ctx, plan.Owner)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	desired = dedupeOwnerEdges(desired)

	if err := r.client.BatchAddOwners(ctx, entityURN, desired); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	// Confirm the pairs landed. batchAddOwners returns a nullable Boolean and
	// the aspect write happens behind it, so a read-back is the only thing that
	// distinguishes "written" from "accepted and dropped".
	if len(desired) > 0 {
		actual, found, err := r.client.GetEntityOwners(ctx, entityURN)
		if err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
		if !found {
			resp.Diagnostics.AddError(
				"Target entity not found",
				fmt.Sprintf("Entity %q does not exist in DataHub after the owner assignment. Verify the entity URN.", entityURN),
			)
			return
		}
		if missing := missingOwnerEdges(desired, actual); len(missing) > 0 {
			resp.Diagnostics.AddError(
				"Owner assignment did not take effect",
				fmt.Sprintf("After assigning owners to %q, these (owner, ownership type) pairs were not present on read back: %s. "+
					"Verify the owner principals and ownership types exist in DataHub.", entityURN, formatOwnerEdges(missing)),
			)
			return
		}
	}

	plan.ID = types.StringValue(entityURN)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *entityOwnershipResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state entityOwnershipResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	entityURN := state.EntityURN.ValueString()
	declared, diags := ownerEdgesFromSet(ctx, state.Owner)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The OpenAPI v3 entity endpoint is MySQL-backed and strongly consistent. A
	// GraphQL read would be OpenSearch-backed and could report a pair written
	// seconds ago as absent, which is how datahub_secret once produced a
	// spurious plan to delete. An absent ownership aspect simply means no owners.
	actual, found, err := r.client.GetEntityOwners(ctx, entityURN)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if !found {
		// The owned entity itself is gone, so the assignment cannot exist.
		resp.State.RemoveResource(ctx)
		return
	}

	ownerSet, diags := ownerSetFromEdges(ctx, retainPresentOwnerEdges(declared, actual))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Owner = ownerSet
	state.ID = types.StringValue(entityURN)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *entityOwnershipResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan, state entityOwnershipResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	entityURN := plan.EntityURN.ValueString()

	desired, diags := ownerEdgesFromSet(ctx, plan.Owner)
	resp.Diagnostics.Append(diags...)
	prior, diags := ownerEdgesFromSet(ctx, state.Owner)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// prior comes from state, so it holds only pairs this resource declared.
	// That is what confines both calls below to this resource's own edges.
	add, remove := diffOwnerEdges(prior, desired)

	if err := r.client.BatchAddOwners(ctx, entityURN, add); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	for _, e := range remove {
		if err := r.client.RemoveOwner(ctx, entityURN, e.OwnerURN, e.OwnershipTypeURN); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	plan.ID = types.StringValue(entityURN)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *entityOwnershipResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state entityOwnershipResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	entityURN := state.EntityURN.ValueString()
	declared, diags := ownerEdgesFromSet(ctx, state.Owner)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Read first, for two reasons that both reduce to removeOwner validating
	// the target entity rather than the owner.
	//
	// If the entity is gone, its owners are gone with it, but removeOwner would
	// refuse with "Resource does not exist" -- and a Delete that errors leaves
	// the resource in state permanently, needing terraform state rm. Read
	// normally catches this first and removes the resource before any destroy
	// plan, so this guard covers the window Read cannot: a refresh that was
	// skipped, or a deletion that lands between refresh and apply.
	//
	// Otherwise, narrowing to the pairs still present skips calls that would
	// achieve nothing. removeOwner does not validate owners, so those calls
	// would succeed either way; not making them is simply cheaper and keeps the
	// request count proportional to what is actually being removed.
	actual, found, err := r.client.GetEntityOwners(ctx, entityURN)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	// Only pairs this resource declared, one removeOwner each with the ownership
	// type pinned so the owner's other ownership types are left alone.
	for _, e := range retainPresentOwnerEdges(declared, actual) {
		if err := r.client.RemoveOwner(ctx, entityURN, e.OwnerURN, e.OwnershipTypeURN); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *entityOwnershipResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	entityURN := strings.TrimSpace(req.ID)
	if entityURN == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			"Expected the target entity's URN (e.g. 'urn:li:glossaryTerm:revenue').",
		)
		return
	}
	if _, _, err := datahub.OwnershipTargetType(entityURN); err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	actual, found, err := r.client.GetEntityOwners(ctx, entityURN)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Entity not found",
			fmt.Sprintf("No entity with URN %q exists in DataHub.", entityURN),
		)
		return
	}
	if len(actual) == 0 {
		resp.Diagnostics.AddError(
			"No owners to import",
			fmt.Sprintf("Entity %q exists but has no owners, so there is nothing for "+
				"datahub_entity_ownership to adopt. Assign the owners with terraform apply instead.", entityURN),
		)
		return
	}

	// Adopt every owner present: the resource's merge contract means anything
	// left undeclared would be invisible to Terraform, and an import that
	// silently dropped owners would then be indistinguishable from one that
	// adopted them.
	ownerSet, diags := ownerSetFromEdges(ctx, dedupeOwnerEdges(actual))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := entityOwnershipResourceModel{
		ID:        types.StringValue(entityURN),
		EntityURN: types.StringValue(entityURN),
		Owner:     ownerSet,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
