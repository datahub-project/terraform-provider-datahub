// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ datasource.DataSource              = &structuredPropertyDataSource{}
	_ datasource.DataSourceWithConfigure = &structuredPropertyDataSource{}
)

type structuredPropertyDataSource struct {
	client *datahub.Client
}

// settingsAttrTypes holds the attribute types for the settings object in the
// data source model.
var settingsAttrTypes = map[string]attr.Type{
	"is_hidden":                        types.BoolType,
	"show_in_search_filters":           types.BoolType,
	"show_in_asset_summary":            types.BoolType,
	"hide_in_asset_summary_when_empty": types.BoolType,
	"show_as_asset_badge":              types.BoolType,
	"show_in_columns_table":            types.BoolType,
}

// structuredPropertyDataSourceModel is the read-back model.
type structuredPropertyDataSourceModel struct {
	PropertyID         types.String `tfsdk:"property_id"`
	URN                types.String `tfsdk:"urn"`
	QualifiedName      types.String `tfsdk:"qualified_name"`
	ValueType          types.String `tfsdk:"value_type"`
	Cardinality        types.String `tfsdk:"cardinality"`
	EntityTypes        types.Set    `tfsdk:"entity_types"`
	AllowedValues      types.List   `tfsdk:"allowed_values"`
	AllowedEntityTypes types.Set    `tfsdk:"allowed_entity_types"`
	DisplayName        types.String `tfsdk:"display_name"`
	Description        types.String `tfsdk:"description"`
	Immutable          types.Bool   `tfsdk:"immutable"`
	Settings           types.Object `tfsdk:"settings"`
}

// NewStructuredPropertyDataSource returns the singular
// datahub_structured_property lookup data source.
func NewStructuredPropertyDataSource() datasource.DataSource {
	return &structuredPropertyDataSource{}
}

func (d *structuredPropertyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_structured_property"
}

func (d *structuredPropertyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Looks up an existing DataHub structured property definition by `property_id`.\n\n" +
			"Use this to reference a structured property that already exists in DataHub -- for " +
			"example one created via the DataHub UI or Python SDK -- without taking ownership of it.",
		Attributes: map[string]schema.Attribute{
			"property_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The unique identifier of the structured property to look up (the URN suffix, e.g. `io.acme.retention`).",
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this structured property.",
			},
			"qualified_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The qualified name of the property.",
			},
			"value_type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Data type for values of this property (e.g. `number`).",
			},
			"cardinality": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Cardinality of values: `SINGLE` or `MULTIPLE`.",
			},
			"entity_types": schema.SetAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Short entity-type names this property can be applied to.",
			},
			"allowed_values": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Allowed values for this property, if constrained.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"string_value": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "String allowed value.",
						},
						"number_value": schema.Float64Attribute{
							Computed:            true,
							MarkdownDescription: "Numeric allowed value.",
						},
						"description": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Description of what this value means.",
						},
					},
				},
			},
			"allowed_entity_types": schema.SetAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Entity types that URN values may reference (when value_type is `urn`).",
			},
			"display_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Human-readable display name.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description of the property.",
			},
			"immutable": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether values are immutable once applied.",
			},
			"settings": schema.SingleNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Display and search settings.",
				Attributes: map[string]schema.Attribute{
					"is_hidden": schema.BoolAttribute{
						Computed:            true,
						MarkdownDescription: "Whether the property is hidden from the UI.",
					},
					"show_in_search_filters": schema.BoolAttribute{
						Computed:            true,
						MarkdownDescription: "Whether the property appears as a search filter facet.",
					},
					"show_in_asset_summary": schema.BoolAttribute{
						Computed:            true,
						MarkdownDescription: "Whether the property appears in the asset summary panel.",
					},
					"hide_in_asset_summary_when_empty": schema.BoolAttribute{
						Computed:            true,
						MarkdownDescription: "Whether to hide the property when no value is set.",
					},
					"show_as_asset_badge": schema.BoolAttribute{
						Computed:            true,
						MarkdownDescription: "Whether the value appears as a badge on asset cards.",
					},
					"show_in_columns_table": schema.BoolAttribute{
						Computed:            true,
						MarkdownDescription: "Whether the property appears in the schema columns table.",
					},
				},
			},
		},
	}
}

func (d *structuredPropertyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*datahub.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *datahub.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	d.client = client
}

func (d *structuredPropertyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var config structuredPropertyDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	propertyID := config.PropertyID.ValueString()
	urn := structuredPropertyURNPrefix + propertyID

	sp, err := d.client.GetStructuredPropertyByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if sp == nil {
		resp.Diagnostics.AddError(
			"Structured property not found",
			fmt.Sprintf("No structured property with ID %q was found in DataHub. Verify the property_id and retry.", propertyID),
		)
		return
	}

	state, diags := spToDataSourceModel(ctx, sp)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// spToDataSourceModel converts a StructuredProperty to the data source model.
func spToDataSourceModel(ctx context.Context, sp *datahub.StructuredProperty) (structuredPropertyDataSourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	state := structuredPropertyDataSourceModel{
		PropertyID:    types.StringValue(sp.ID),
		URN:           types.StringValue(sp.URN),
		QualifiedName: types.StringValue(sp.QualifiedName),
		ValueType:     types.StringValue(sp.ValueType),
		Cardinality:   types.StringValue(sp.Cardinality),
		DisplayName:   types.StringValue(sp.DisplayName),
		Description:   types.StringValue(sp.Description),
		Immutable:     types.BoolValue(sp.Immutable),
	}

	entityTypesSet, d := stringsToSet(ctx, sp.EntityTypes, false)
	diags.Append(d...)
	state.EntityTypes = entityTypesSet

	// allowed_values
	if len(sp.AllowedValues) == 0 {
		state.AllowedValues = types.ListNull(types.ObjectType{AttrTypes: allowedValueAttrTypes})
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
				"description":  types.StringValue(av.Description),
			})
			diags.Append(d...)
			avObjs[i] = obj
		}
		avList, d := types.ListValue(types.ObjectType{AttrTypes: allowedValueAttrTypes}, avObjs)
		diags.Append(d...)
		state.AllowedValues = avList
	}

	// allowed_entity_types
	if len(sp.AllowedEntityTypes) == 0 {
		state.AllowedEntityTypes = types.SetNull(types.StringType)
	} else {
		aetSet, d := stringsToSet(ctx, sp.AllowedEntityTypes, false)
		diags.Append(d...)
		state.AllowedEntityTypes = aetSet
	}

	// settings: always present as an object (null if no settings aspect)
	if sp.Settings == nil {
		state.Settings = types.ObjectNull(settingsAttrTypes)
	} else {
		s := sp.Settings
		settingsObj, d := types.ObjectValue(settingsAttrTypes, map[string]attr.Value{
			"is_hidden":                        types.BoolValue(s.IsHidden),
			"show_in_search_filters":           types.BoolValue(s.ShowInSearchFilters),
			"show_in_asset_summary":            types.BoolValue(s.ShowInAssetSummary),
			"hide_in_asset_summary_when_empty": types.BoolValue(s.HideInAssetSummaryWhenEmpty),
			"show_as_asset_badge":              types.BoolValue(s.ShowAsAssetBadge),
			"show_in_columns_table":            types.BoolValue(s.ShowInColumnsTable),
		})
		diags.Append(d...)
		state.Settings = settingsObj
	}

	return state, diags
}
