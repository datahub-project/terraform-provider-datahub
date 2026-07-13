// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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

const structuredPropertyURNPrefix = "urn:li:structuredProperty:"

var (
	_ resource.Resource                = &structuredPropertyResource{}
	_ resource.ResourceWithConfigure   = &structuredPropertyResource{}
	_ resource.ResourceWithImportState = &structuredPropertyResource{}
)

// valueTypeValidator enforces that value_type is one of the five bootstrapped
// DataHub data types: string, number, date, urn, rich_text.
type valueTypeValidator struct{}

func (v valueTypeValidator) Description(_ context.Context) string {
	return `must be one of: string, number, date, urn, rich_text`
}

func (v valueTypeValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v valueTypeValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	val := req.ConfigValue.ValueString()
	valid := map[string]bool{"string": true, "number": true, "date": true, "urn": true, "rich_text": true}
	if !valid[val] {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid value_type",
			fmt.Sprintf("%q is not a valid value_type. Must be one of: string, number, date, urn, rich_text.", val),
		)
	}
}

// cardinalityValidator enforces that cardinality is SINGLE or MULTIPLE.
type cardinalityValidator struct{}

func (v cardinalityValidator) Description(_ context.Context) string {
	return `must be "SINGLE" or "MULTIPLE"`
}

func (v cardinalityValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v cardinalityValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	val := req.ConfigValue.ValueString()
	if val != "SINGLE" && val != "MULTIPLE" {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid cardinality",
			fmt.Sprintf("%q is not a valid cardinality. Must be \"SINGLE\" or \"MULTIPLE\".", val),
		)
	}
}

// requiresReplaceIfNarrowedModifier is a plan modifier for cardinality: forces
// resource replacement when state is "MULTIPLE" and plan is "SINGLE", because
// the DataHub API cannot narrow cardinality.
type requiresReplaceIfNarrowedModifier struct{}

func (m requiresReplaceIfNarrowedModifier) Description(_ context.Context) string {
	return "Requires replacement when cardinality is narrowed from MULTIPLE to SINGLE."
}

func (m requiresReplaceIfNarrowedModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m requiresReplaceIfNarrowedModifier) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}
	if req.PlanValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}
	if req.StateValue.ValueString() == "MULTIPLE" && req.PlanValue.ValueString() == "SINGLE" {
		resp.RequiresReplace = true
	}
}

// requiresReplaceIfSetShrunkModifier is a plan modifier for set attributes that
// are append-only in the DataHub API. It forces replacement when any element
// present in the state set is absent from the plan set.
type requiresReplaceIfSetShrunkModifier struct{}

func (m requiresReplaceIfSetShrunkModifier) Description(_ context.Context) string {
	return "Requires replacement when the set shrinks (elements cannot be removed via the DataHub API)."
}

func (m requiresReplaceIfSetShrunkModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m requiresReplaceIfSetShrunkModifier) PlanModifySet(_ context.Context, req planmodifier.SetRequest, resp *planmodifier.SetResponse) {
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}
	if req.PlanValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}
	// Build a set of plan element strings for O(1) lookup.
	planElements := make(map[string]bool)
	for _, e := range req.PlanValue.Elements() {
		planElements[e.String()] = true
	}
	for _, e := range req.StateValue.Elements() {
		if !planElements[e.String()] {
			resp.RequiresReplace = true
			return
		}
	}
}

// requiresReplaceIfListShrunkModifier forces replacement when an allowed_values
// list shrinks (the DataHub update mutation only appends).
type requiresReplaceIfListShrunkModifier struct{}

func (m requiresReplaceIfListShrunkModifier) Description(_ context.Context) string {
	return "Requires replacement when the list shrinks (values cannot be removed via the DataHub API)."
}

func (m requiresReplaceIfListShrunkModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m requiresReplaceIfListShrunkModifier) PlanModifyList(_ context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}
	if req.PlanValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}
	if len(req.PlanValue.Elements()) < len(req.StateValue.Elements()) {
		resp.RequiresReplace = true
	}
}

type structuredPropertyResource struct {
	client *datahub.Client
}

// structuredPropertySettingsModel maps to the settings nested block.
type structuredPropertySettingsModel struct {
	IsHidden                    types.Bool `tfsdk:"is_hidden"`
	ShowInSearchFilters         types.Bool `tfsdk:"show_in_search_filters"`
	ShowInAssetSummary          types.Bool `tfsdk:"show_in_asset_summary"`
	HideInAssetSummaryWhenEmpty types.Bool `tfsdk:"hide_in_asset_summary_when_empty"`
	ShowAsAssetBadge            types.Bool `tfsdk:"show_as_asset_badge"`
	ShowInColumnsTable          types.Bool `tfsdk:"show_in_columns_table"`
}

// allowedValueModel maps to a single allowed_values list element.
type allowedValueModel struct {
	StringValue types.String  `tfsdk:"string_value"`
	NumberValue types.Float64 `tfsdk:"number_value"`
	Description types.String  `tfsdk:"description"`
}

type structuredPropertyResourceModel struct {
	ID                 types.String                     `tfsdk:"id"`
	URN                types.String                     `tfsdk:"urn"`
	QualifiedName      types.String                     `tfsdk:"qualified_name"`
	PropertyID         types.String                     `tfsdk:"property_id"`
	ValueType          types.String                     `tfsdk:"value_type"`
	Cardinality        types.String                     `tfsdk:"cardinality"`
	EntityTypes        types.Set                        `tfsdk:"entity_types"`
	AllowedValues      types.List                       `tfsdk:"allowed_values"`
	AllowedEntityTypes types.Set                        `tfsdk:"allowed_entity_types"`
	DisplayName        types.String                     `tfsdk:"display_name"`
	Description        types.String                     `tfsdk:"description"`
	Immutable          types.Bool                       `tfsdk:"immutable"`
	Settings           *structuredPropertySettingsModel `tfsdk:"settings"`
}

// allowedValueAttrTypes holds the attribute types for the allowed_values list
// element object type. Used when building null/empty list values.
var allowedValueAttrTypes = map[string]attr.Type{
	"string_value": types.StringType,
	"number_value": types.Float64Type,
	"description":  types.StringType,
}

func NewStructuredPropertyResource() resource.Resource {
	return &structuredPropertyResource{}
}

func (r *structuredPropertyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	client := pd.Client
	r.client = client
}

func (r *structuredPropertyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_structured_property"
}

func (r *structuredPropertyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a DataHub structured property definition.\n\n" +
			"A structured property defines a typed, named custom property that can be applied to " +
			"data assets in DataHub (e.g. a `number`-valued `retentionDays` applicable to datasets). " +
			"This resource manages the *definition* only -- the schema object that declares the property " +
			"type, cardinality, entity applicability, and allowed values. Applying property values to " +
			"individual assets is per-asset enrichment and is outside the scope of this provider.\n\n" +
			"## Naming\n\n" +
			"`property_id` becomes both the URN suffix (`urn:li:structuredProperty:<property_id>`) and " +
			"the `qualifiedName`. Use a dotted reverse-DNS form (e.g. `io.acme.privacy.classification`) " +
			"to avoid collisions and to match the convention used by the DataHub Python SDK. A " +
			"Terraform-managed property and a Python-SDK-created property with the same `property_id` " +
			"reference the same DataHub entity.\n\n" +
			"## Update semantics\n\n" +
			"DataHub's `updateStructuredProperty` mutation is append-only for `entity_types`, " +
			"`allowed_values`, and `allowed_entity_types`, and cardinality can only widen from " +
			"`SINGLE` to `MULTIPLE`. Additive changes (adding elements to these lists, or widening " +
			"cardinality) are applied in-place. If you remove an element from any of these lists or " +
			"narrow cardinality from `MULTIPLE` to `SINGLE`, Terraform will destroy and recreate the " +
			"property. **Destroying a property hard-deletes it and removes all applied values from " +
			"every asset.**\n\n" +
			"## Value types\n\n" +
			"Specify `value_type` as a short name: `string`, `number`, `date`, `urn`, or `rich_text`. " +
			"When `value_type = \"urn\"`, use `allowed_entity_types` to restrict which entity types " +
			"the URN values may reference.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this structured property (e.g., `urn:li:structuredProperty:io.acme.retention`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"qualified_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The qualified name of the property (equal to `property_id`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"property_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Unique identifier for the structured property. Becomes the URN suffix and `qualifiedName` " +
					"(`urn:li:structuredProperty:<property_id>`). Use a dotted reverse-DNS form " +
					"(e.g. `io.acme.privacy.classification`). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"value_type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Data type for values of this property. One of: `string`, `number`, `date`, `urn`, `rich_text`. " +
					"Changing this forces a new resource.",
				Validators: []validator.String{
					valueTypeValidator{},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"cardinality": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("SINGLE"),
				MarkdownDescription: "Whether each asset may have one (`SINGLE`) or multiple (`MULTIPLE`) values for this property. Defaults to `SINGLE`. Widening from `SINGLE` to `MULTIPLE` is applied in-place; narrowing forces a new resource.",
				Validators: []validator.String{
					cardinalityValidator{},
				},
				PlanModifiers: []planmodifier.String{
					requiresReplaceIfNarrowedModifier{},
				},
			},
			"entity_types": schema.SetAttribute{
				Required:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Set of entity type short names this property can be applied to (e.g. `[\"dataset\", \"dashboard\"]`). " +
					"Corresponds to `urn:li:entityType:datahub.<name>` URNs. Adding entity types is applied in-place; removing any forces a new resource.",
				PlanModifiers: []planmodifier.Set{
					requiresReplaceIfSetShrunkModifier{},
				},
			},
			"allowed_values": schema.ListNestedAttribute{
				Optional: true,
				MarkdownDescription: "Optional list of allowed values. When set, only values in this list may be applied to assets. " +
					"Each entry specifies exactly one of `string_value` or `number_value` and an optional `description`. " +
					"Adding values is applied in-place; removing any forces a new resource.",
				PlanModifiers: []planmodifier.List{
					requiresReplaceIfListShrunkModifier{},
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"string_value": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "String allowed value. Set exactly one of `string_value` or `number_value`.",
						},
						"number_value": schema.Float64Attribute{
							Optional:            true,
							MarkdownDescription: "Numeric allowed value. Set exactly one of `string_value` or `number_value`.",
						},
						"description": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Human-readable description of what this value means.",
						},
					},
				},
			},
			"allowed_entity_types": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Entity type short names that URN values may reference. Only meaningful when `value_type = \"urn\"`. " +
					"E.g. `[\"corpuser\", \"corpGroup\"]`. Adding entity types is applied in-place; removing any forces a new resource.",
				PlanModifiers: []planmodifier.Set{
					requiresReplaceIfSetShrunkModifier{},
				},
			},
			"display_name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Human-readable display name shown in the DataHub UI. Defaults to `property_id` if not set.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the property's purpose and intended usage.",
			},
			"immutable": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Whether values applied to assets are immutable once set. Defaults to `false`.",
			},
			"settings": schema.SingleNestedAttribute{
				Optional: true,
				MarkdownDescription: "Display and search settings for this property in the DataHub UI. " +
					"If `is_hidden` is true, all other flags must be false.",
				Attributes: map[string]schema.Attribute{
					"is_hidden": schema.BoolAttribute{
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(false),
						MarkdownDescription: "Hide this property from the DataHub UI entirely. Defaults to `false`.",
					},
					"show_in_search_filters": schema.BoolAttribute{
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(false),
						MarkdownDescription: "Show this property as a search filter facet. Defaults to `false`.",
					},
					"show_in_asset_summary": schema.BoolAttribute{
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(false),
						MarkdownDescription: "Show this property in the asset summary panel. Defaults to `false`.",
					},
					"hide_in_asset_summary_when_empty": schema.BoolAttribute{
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(false),
						MarkdownDescription: "Hide the property from the asset summary when no value is set. Defaults to `false`.",
					},
					"show_as_asset_badge": schema.BoolAttribute{
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(false),
						MarkdownDescription: "Show the property value as a badge on asset cards. Defaults to `false`.",
					},
					"show_in_columns_table": schema.BoolAttribute{
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(false),
						MarkdownDescription: "Show the property in the schema columns table. Defaults to `false`.",
					},
				},
			},
		},
	}
}

func (r *structuredPropertyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan structuredPropertyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, diags := createInputFromModel(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn, err := r.client.CreateStructuredProperty(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = types.StringValue(urn)
	plan.URN = types.StringValue(urn)
	plan.QualifiedName = types.StringValue(plan.PropertyID.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *structuredPropertyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state structuredPropertyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}

	sp, err := r.client.GetStructuredPropertyByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if sp == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	diags := applyStructuredPropertyToModel(ctx, sp, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *structuredPropertyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan, state structuredPropertyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()

	// The full desired state is written as a single OpenAPI aspect upsert (see
	// the client). Plan modifiers force replacement on any list shrink or
	// cardinality narrowing, so the plan here is always a superset/scalar change.
	in, diags := createInputFromModel(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.UpdateStructuredProperty(ctx, urn, in); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = state.ID
	plan.URN = state.URN
	plan.QualifiedName = state.QualifiedName
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *structuredPropertyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state structuredPropertyResourceModel
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

	if err := r.client.DeleteStructuredProperty(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *structuredPropertyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub structured property URN (e.g., urn:li:structuredProperty:io.acme.retention) or a bare property ID.")
		return
	}

	var propertyID, urn string
	if strings.HasPrefix(raw, structuredPropertyURNPrefix) {
		urn = raw
		propertyID = strings.TrimPrefix(raw, structuredPropertyURNPrefix)
	} else {
		propertyID = raw
		urn = structuredPropertyURNPrefix + propertyID
	}
	if propertyID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub structured property URN (e.g., urn:li:structuredProperty:io.acme.retention) or a bare property ID.")
		return
	}

	sp, err := r.client.GetStructuredPropertyByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if sp == nil {
		resp.Diagnostics.AddError(
			"Structured property not found",
			fmt.Sprintf("No structured property with URN %q was found in DataHub. Verify the ID or URN and retry.", urn),
		)
		return
	}

	state := structuredPropertyResourceModel{
		ID:         types.StringValue(sp.URN),
		URN:        types.StringValue(sp.URN),
		PropertyID: types.StringValue(sp.ID),
	}
	diags := applyStructuredPropertyToModel(ctx, sp, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// ---------- helper: create input from plan model ----------

func createInputFromModel(ctx context.Context, m *structuredPropertyResourceModel) (datahub.CreateStructuredPropertyInput, diag.Diagnostics) {
	var diags diag.Diagnostics
	in := datahub.CreateStructuredPropertyInput{
		ID:          m.PropertyID.ValueString(),
		DisplayName: strVal(m.DisplayName),
		Description: strVal(m.Description),
		ValueType:   m.ValueType.ValueString(),
		Cardinality: m.Cardinality.ValueString(),
		Immutable:   m.Immutable.ValueBool(),
	}

	entityTypes, d := setToStrings(ctx, m.EntityTypes)
	diags.Append(d...)
	in.EntityTypes = entityTypes

	if !m.AllowedValues.IsNull() && !m.AllowedValues.IsUnknown() {
		var avModels []allowedValueModel
		diags.Append(m.AllowedValues.ElementsAs(ctx, &avModels, false)...)
		in.AllowedValues = modelsToAllowedValues(avModels)
	}

	if !m.AllowedEntityTypes.IsNull() && !m.AllowedEntityTypes.IsUnknown() {
		allowedTypes, d := setToStrings(ctx, m.AllowedEntityTypes)
		diags.Append(d...)
		in.AllowedEntityTypes = allowedTypes
	}

	if m.Settings != nil {
		in.Settings = settingsModelToClient(m.Settings)
	}

	return in, diags
}

// modelsToAllowedValues converts []allowedValueModel to []datahub.AllowedValue.
func modelsToAllowedValues(models []allowedValueModel) []datahub.AllowedValue {
	result := make([]datahub.AllowedValue, len(models))
	for i, m := range models {
		av := datahub.AllowedValue{
			Description: strVal(m.Description),
		}
		if !m.StringValue.IsNull() && !m.StringValue.IsUnknown() {
			s := m.StringValue.ValueString()
			av.StringValue = &s
		}
		if !m.NumberValue.IsNull() && !m.NumberValue.IsUnknown() {
			n := m.NumberValue.ValueFloat64()
			av.NumberValue = &n
		}
		result[i] = av
	}
	return result
}

// settingsModelToClient converts the settings nested model to the client type.
func settingsModelToClient(m *structuredPropertySettingsModel) *datahub.StructuredPropertySettings {
	if m == nil {
		return nil
	}
	return &datahub.StructuredPropertySettings{
		IsHidden:                    m.IsHidden.ValueBool(),
		ShowInSearchFilters:         m.ShowInSearchFilters.ValueBool(),
		ShowInAssetSummary:          m.ShowInAssetSummary.ValueBool(),
		HideInAssetSummaryWhenEmpty: m.HideInAssetSummaryWhenEmpty.ValueBool(),
		ShowAsAssetBadge:            m.ShowAsAssetBadge.ValueBool(),
		ShowInColumnsTable:          m.ShowInColumnsTable.ValueBool(),
	}
}

// ---------- applyStructuredPropertyToModel ----------

// applyStructuredPropertyToModel maps a read StructuredProperty onto the
// resource model, normalising empty optional fields to null.
func applyStructuredPropertyToModel(ctx context.Context, sp *datahub.StructuredProperty, m *structuredPropertyResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	m.URN = types.StringValue(sp.URN)
	m.ID = types.StringValue(sp.URN)
	m.QualifiedName = types.StringValue(sp.QualifiedName)
	m.PropertyID = types.StringValue(sp.ID)
	m.ValueType = types.StringValue(sp.ValueType)
	m.Cardinality = types.StringValue(sp.Cardinality)
	m.Immutable = types.BoolValue(sp.Immutable)
	m.DisplayName = nullIfEmpty(sp.DisplayName)
	m.Description = nullIfEmpty(sp.Description)

	// entity_types: always a non-null set.
	entityTypesSet, d := stringsToSet(ctx, sp.EntityTypes, false)
	diags.Append(d...)
	m.EntityTypes = entityTypesSet

	// allowed_values: null list when empty.
	if len(sp.AllowedValues) == 0 {
		m.AllowedValues = types.ListNull(types.ObjectType{AttrTypes: allowedValueAttrTypes})
	} else {
		avObjs := make([]attr.Value, len(sp.AllowedValues))
		for i, av := range sp.AllowedValues {
			svVal := types.StringNull()
			if av.StringValue != nil {
				svVal = types.StringValue(*av.StringValue)
			}
			nvVal := types.Float64Null()
			if av.NumberValue != nil {
				nvVal = types.Float64Value(*av.NumberValue)
			}
			obj, d := types.ObjectValue(allowedValueAttrTypes, map[string]attr.Value{
				"string_value": svVal,
				"number_value": nvVal,
				"description":  nullIfEmpty(av.Description),
			})
			diags.Append(d...)
			avObjs[i] = obj
		}
		avList, d := types.ListValue(types.ObjectType{AttrTypes: allowedValueAttrTypes}, avObjs)
		diags.Append(d...)
		m.AllowedValues = avList
	}

	// allowed_entity_types: null set when empty.
	if len(sp.AllowedEntityTypes) == 0 {
		m.AllowedEntityTypes = types.SetNull(types.StringType)
	} else {
		aetSet, d := stringsToSet(ctx, sp.AllowedEntityTypes, false)
		diags.Append(d...)
		m.AllowedEntityTypes = aetSet
	}

	// settings: nil pointer when no settings aspect.
	if sp.Settings == nil {
		m.Settings = nil
	} else {
		m.Settings = &structuredPropertySettingsModel{
			IsHidden:                    types.BoolValue(sp.Settings.IsHidden),
			ShowInSearchFilters:         types.BoolValue(sp.Settings.ShowInSearchFilters),
			ShowInAssetSummary:          types.BoolValue(sp.Settings.ShowInAssetSummary),
			HideInAssetSummaryWhenEmpty: types.BoolValue(sp.Settings.HideInAssetSummaryWhenEmpty),
			ShowAsAssetBadge:            types.BoolValue(sp.Settings.ShowAsAssetBadge),
			ShowInColumnsTable:          types.BoolValue(sp.Settings.ShowInColumnsTable),
		}
	}

	return diags
}
