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

const spAssignmentIDSeparator = "|"

var (
	_ resource.Resource                = &structuredPropertyAssignmentResource{}
	_ resource.ResourceWithConfigure   = &structuredPropertyAssignmentResource{}
	_ resource.ResourceWithImportState = &structuredPropertyAssignmentResource{}
)

// supportedAssignmentTargetValidator rejects entity_urn values whose entity type
// the provider does not support as a structured-property assignment target.
type supportedAssignmentTargetValidator struct{}

func (v supportedAssignmentTargetValidator) Description(_ context.Context) string {
	return "must be the URN of a supported target entity type: " + strings.Join(datahub.SupportedAssignmentEntityTypes(), ", ")
}

func (v supportedAssignmentTargetValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v supportedAssignmentTargetValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	// CAT-2562: DataHub silently NOOPs (HTTP 200, nothing persisted) when the
	// structuredProperties aspect is written to an entity type that does not
	// register it, so without this plan-time guard a user would get a success
	// that does nothing. The provider charter also excludes per-asset /
	// ingested-entity enrichment (datasets, charts, dashboards, ...).
	if _, _, err := datahub.AssignmentTargetType(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Unsupported assignment target", err.Error())
	}
}

type structuredPropertyAssignmentResource struct {
	client *datahub.Client
}

type structuredPropertyAssignmentResourceModel struct {
	ID                    types.String `tfsdk:"id"`
	EntityURN             types.String `tfsdk:"entity_urn"`
	StructuredPropertyURN types.String `tfsdk:"structured_property_urn"`
	Values                types.Set    `tfsdk:"values"`
}

func NewStructuredPropertyAssignmentResource() resource.Resource {
	return &structuredPropertyAssignmentResource{}
}

func (r *structuredPropertyAssignmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	client := pd.Client
	r.client = client
}

func (r *structuredPropertyAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_structured_property_assignment"
}

func (r *structuredPropertyAssignmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Assigns a structured property's value(s) to a target entity. Each resource models a " +
			"single `(entity, property)` edge: one target entity, one structured property, and its " +
			"list of values.\n\n" +
			"Assignments **merge** per property -- creating, updating, or deleting one assignment " +
			"leaves any other structured properties on the same entity untouched, so multiple " +
			"`datahub_structured_property_assignment` resources may safely target the same entity " +
			"(one per property). Values are validated by DataHub against the property's cardinality " +
			"and allowed values.\n\n" +
			"Supported target entity types: `domain`, `glossaryNode`, `glossaryTerm`, `dataProduct` " +
			"(platform-governance entities). Assigning structured properties to ingested data assets " +
			"(datasets, dashboards, ...) is out of scope for this provider and is rejected.\n\n" +
			"## References\n\n" +
			"Prefer expression inputs: set `structured_property_urn` to " +
			"`datahub_structured_property.<name>.urn` and `entity_urn` to the target's `.urn` " +
			"(e.g. `datahub_domain.<name>.urn`), so the property and target are created first.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Composite identifier: `<entity_urn>|<structured_property_urn>`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"entity_urn": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "URN of the target entity to assign the property to. Must be a `domain`, `glossaryNode`, `glossaryTerm`, or `dataProduct` URN. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					supportedAssignmentTargetValidator{},
				},
			},
			"structured_property_urn": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "URN of the structured property to assign (e.g. `urn:li:structuredProperty:<id>`). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"values": schema.SetAttribute{
				Required:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "The value(s) to assign, as an unordered set. A `SINGLE`-cardinality property takes exactly one; a `MULTIPLE` property takes several. All values are strings; for a `number`-typed property give the number in minimal string form (e.g. `\"30\"`). Can be changed in place. DataHub does not preserve value ordering, so this is modelled as a set: reordering the values produces no diff.",
			},
		},
	}
}

func (r *structuredPropertyAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan structuredPropertyAssignmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	entityURN := plan.EntityURN.ValueString()
	propertyURN := plan.StructuredPropertyURN.ValueString()

	var values []string
	resp.Diagnostics.Append(plan.Values.ElementsAs(ctx, &values, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	valueType, ok := r.assertApplicable(ctx, entityURN, propertyURN, resp.Diagnostics.AddError)
	if !ok {
		return
	}

	if err := r.client.SetStructuredPropertyValues(ctx, entityURN, propertyURN, valueType, values); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	// Confirm the assignment landed (guards against a silent no-op).
	if _, found, err := r.client.GetStructuredPropertyValues(ctx, entityURN, propertyURN); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	} else if !found {
		resp.Diagnostics.AddError(
			"Structured property assignment did not take effect",
			fmt.Sprintf("After assigning %q to %q, no value was present on read back. Verify the target entity exists in DataHub.", propertyURN, entityURN),
		)
		return
	}

	plan.ID = types.StringValue(spAssignmentID(entityURN, propertyURN))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *structuredPropertyAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state structuredPropertyAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	entityURN := state.EntityURN.ValueString()
	propertyURN := state.StructuredPropertyURN.ValueString()

	values, found, err := r.client.GetStructuredPropertyValues(ctx, entityURN, propertyURN)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	valuesSet, diags := types.SetValueFrom(ctx, types.StringType, values)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Values = valuesSet
	state.ID = types.StringValue(spAssignmentID(entityURN, propertyURN))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *structuredPropertyAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan structuredPropertyAssignmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	entityURN := plan.EntityURN.ValueString()
	propertyURN := plan.StructuredPropertyURN.ValueString()

	var values []string
	resp.Diagnostics.Append(plan.Values.ElementsAs(ctx, &values, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	valueType, ok := r.assertApplicable(ctx, entityURN, propertyURN, resp.Diagnostics.AddError)
	if !ok {
		return
	}

	if err := r.client.SetStructuredPropertyValues(ctx, entityURN, propertyURN, valueType, values); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = types.StringValue(spAssignmentID(entityURN, propertyURN))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *structuredPropertyAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state structuredPropertyAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.RemoveStructuredProperty(ctx, state.EntityURN.ValueString(), state.StructuredPropertyURN.ValueString()); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *structuredPropertyAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	entityURN, propertyURN, found := strings.Cut(strings.TrimSpace(req.ID), spAssignmentIDSeparator)
	entityURN = strings.TrimSpace(entityURN)
	propertyURN = strings.TrimSpace(propertyURN)
	if !found || entityURN == "" || propertyURN == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			"Expected a composite ID of the form '<entity_urn>|<structured_property_urn>' "+
				"(e.g. 'urn:li:domain:finance|urn:li:structuredProperty:classification').",
		)
		return
	}
	if _, _, err := datahub.AssignmentTargetType(entityURN); err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	values, ok, err := r.client.GetStructuredPropertyValues(ctx, entityURN, propertyURN)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if !ok {
		resp.Diagnostics.AddError(
			"Structured property assignment not found",
			fmt.Sprintf("Entity %q has no value assigned for structured property %q.", entityURN, propertyURN),
		)
		return
	}

	valuesSet, diags := types.SetValueFrom(ctx, types.StringType, values)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := structuredPropertyAssignmentResourceModel{
		ID:                    types.StringValue(spAssignmentID(entityURN, propertyURN)),
		EntityURN:             types.StringValue(entityURN),
		StructuredPropertyURN: types.StringValue(propertyURN),
		Values:                valuesSet,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// assertApplicable fetches the property definition and returns its value type
// (for value routing). It returns false if a diagnostic was raised.
func (r *structuredPropertyAssignmentResource) assertApplicable(ctx context.Context, entityURN, propertyURN string, addError func(string, string)) (string, bool) {
	def, err := r.client.GetStructuredPropertyByURN(ctx, propertyURN)
	if err != nil {
		addError("DataHub API Error", err.Error())
		return "", false
	}
	if def == nil {
		addError(
			"Structured property not found",
			fmt.Sprintf("No structured property with URN %q exists. Create the definition (datahub_structured_property) before assigning it.", propertyURN),
		)
		return "", false
	}

	if _, _, err := datahub.AssignmentTargetType(entityURN); err != nil {
		addError("Unsupported assignment target", err.Error())
		return "", false
	}

	return def.ValueType, true
}

func spAssignmentID(entityURN, propertyURN string) string {
	return entityURN + spAssignmentIDSeparator + propertyURN
}
