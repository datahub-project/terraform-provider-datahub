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
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ resource.Resource                = &schemaAssertionResource{}
	_ resource.ResourceWithConfigure   = &schemaAssertionResource{}
	_ resource.ResourceWithImportState = &schemaAssertionResource{}
)

type schemaAssertionResource struct {
	client *datahub.Client
}

type schemaAssertionResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	URN                types.String `tfsdk:"urn"`
	EntityURN          types.String `tfsdk:"entity_urn"`
	Description        types.String `tfsdk:"description"`
	Compatibility      types.String `tfsdk:"compatibility"`
	Fields             types.List   `tfsdk:"fields"`
	EvaluationCron     types.String `tfsdk:"evaluation_cron"`
	EvaluationTimezone types.String `tfsdk:"evaluation_timezone"`
	OnSuccessActions   types.List   `tfsdk:"on_success_actions"`
	OnFailureActions   types.List   `tfsdk:"on_failure_actions"`
	Mode               types.String `tfsdk:"mode"`
	ExecutorID         types.String `tfsdk:"executor_id"`
}

type schemaFieldModel struct {
	Path       types.String `tfsdk:"path"`
	Type       types.String `tfsdk:"type"`
	NativeType types.String `tfsdk:"native_type"`
}

// schemaFieldObjectType is the object type of one entry in the fields list.
var schemaFieldObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"path":        types.StringType,
	"type":        types.StringType,
	"native_type": types.StringType,
}}

// NewSchemaAssertionResource returns a new datahub_schema_assertion resource.
func NewSchemaAssertionResource() resource.Resource {
	return &schemaAssertionResource{}
}

func (r *schemaAssertionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	client := pd.Client
	r.client = client
}

func (r *schemaAssertionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_schema_assertion"
}

func (r *schemaAssertionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: cloudOnlyBadge +
			"Creates and manages a DataHub schema assertion monitor on a dataset.\n\n" +
			"Schema assertions check that a dataset's columns match an expected set, " +
			"catching unexpected schema drift. The resource owns the complete expected " +
			"field list and the comparison `compatibility` mode.\n\n" +
			"## Compatibility\n\n" +
			"`EXACT_MATCH` requires the actual schema to equal the expected fields exactly. " +
			"`SUPERSET` allows the actual schema to contain additional fields. `SUBSET` " +
			"allows it to contain a subset.\n\n" +
			"## URN\n\n" +
			"DataHub generates a server-side UUID for each assertion. The `urn` and `id` " +
			"attributes are populated after creation and are stable across updates. " +
			"ImportState requires the full assertion URN (e.g. `urn:li:assertion:<uuid>`). " +
			"Only NATIVE (author-as-code) assertions can be imported; ingested EXTERNAL " +
			"or smart/AI INFERRED assertions are refused.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this assertion (e.g. `urn:li:assertion:<uuid>`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"entity_urn": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "URN of the DataHub dataset this assertion monitors.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Human-readable description of what this schema assertion checks.",
			},
			"compatibility": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Schema match mode: `EXACT_MATCH`, `SUPERSET`, or `SUBSET`.",
			},
			"fields": schema.ListNestedAttribute{
				Required: true,
				MarkdownDescription: "The expected columns, in order. The resource owns the complete " +
					"list -- fields added to the dataset outside Terraform are evaluated against " +
					"this set per the `compatibility` mode.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"path": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Column path (field name).",
						},
						"type": schema.StringAttribute{
							Required: true,
							MarkdownDescription: "DataHub standard type for the column: `STRING`, `NUMBER`, " +
								"`BOOLEAN`, `DATE`, `TIME`, `BYTES`, `ENUM`, `FIXED`, `MAP`, `ARRAY`, " +
								"`UNION`, `STRUCT`, or `NULL`.",
						},
						"native_type": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Platform-native column type (e.g. `VARCHAR`, `INTEGER`).",
						},
					},
				},
			},
			"evaluation_cron": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Cron expression defining when DataHub evaluates this assertion.",
			},
			"evaluation_timezone": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Timezone for the evaluation cron schedule (e.g. `\"UTC\"`).",
			},
			"on_success_actions": schema.ListAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Actions to take when the assertion passes (e.g. `[\"RESOLVE_INCIDENT\"]`).",
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"on_failure_actions": schema.ListAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Actions to take when the assertion fails (e.g. `[\"RAISE_INCIDENT\"]`).",
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"mode": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Monitoring mode. `ACTIVE` enables scheduled evaluation. `PASSIVE` records results without scheduling.",
			},
			"executor_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "ID of the remote executor pool to use for evaluation. Omit to use the default executor.",
			},
		},
	}
}

// fieldsToInput converts the fields list attribute to the client input slice.
func fieldsToInput(ctx context.Context, l types.List) ([]datahub.SchemaFieldInput, diag.Diagnostics) {
	var models []schemaFieldModel
	diags := l.ElementsAs(ctx, &models, false)
	if diags.HasError() {
		return nil, diags
	}
	out := make([]datahub.SchemaFieldInput, len(models))
	for i, m := range models {
		out[i] = datahub.SchemaFieldInput{
			Path:       m.Path.ValueString(),
			StdType:    m.Type.ValueString(),
			NativeType: strVal(m.NativeType),
		}
	}
	return out, diags
}

// fieldsFromInfo converts the read-shape field list to the list attribute value.
func fieldsFromInfo(ctx context.Context, fields []datahub.SchemaAssertionField) (types.List, diag.Diagnostics) {
	models := make([]schemaFieldModel, len(fields))
	for i, f := range fields {
		models[i] = schemaFieldModel{
			Path:       types.StringValue(f.Path),
			Type:       types.StringValue(f.StdType),
			NativeType: nullIfEmpty(f.NativeType),
		}
	}
	return types.ListValueFrom(ctx, schemaFieldObjectType, models)
}

func (r *schemaAssertionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan schemaAssertionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, d := r.buildInput(ctx, plan, "")
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn, err := r.client.UpsertSchemaAssertion(ctx, in)
	if err != nil {
		if errors.Is(err, datahub.ErrAssertionCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required",
				"datahub_schema_assertion requires DataHub Cloud. "+
					"The configured DataHub instance does not support assertion monitor management.")
			return
		}
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = types.StringValue(urn)
	plan.URN = types.StringValue(urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *schemaAssertionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state schemaAssertionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}

	ai, err := r.client.GetAssertionByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if ai == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	if d, bad := nonNativeAssertionError(urn, ai.Source); bad {
		resp.Diagnostics.Append(d)
		return
	}

	if ai.Schema != nil {
		state.Compatibility = types.StringValue(ai.Schema.Compatibility)
		fields, d := fieldsFromInfo(ctx, ai.Schema.Fields)
		resp.Diagnostics.Append(d...)
		state.Fields = fields
	}
	state.Description = nullIfEmpty(ai.Description)
	state.EntityURN = types.StringValue(ai.EntityURN)
	state.URN = types.StringValue(ai.URN)
	state.ID = types.StringValue(ai.URN)

	onSuccess, d := stringsToList(ctx, ai.OnSuccessActions)
	resp.Diagnostics.Append(d...)
	state.OnSuccessActions = onSuccess
	onFailure, d := stringsToList(ctx, ai.OnFailureActions)
	resp.Diagnostics.Append(d...)
	state.OnFailureActions = onFailure

	mon, err := r.client.GetAssertionMonitor(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if mon != nil {
		state.EvaluationCron = nullIfEmpty(mon.EvaluationCron)
		state.EvaluationTimezone = nullIfEmpty(mon.EvaluationTimezone)
		if mon.Mode != "" {
			state.Mode = types.StringValue(mon.Mode)
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *schemaAssertionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan, state schemaAssertionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, d := r.buildInput(ctx, plan, state.URN.ValueString())
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.UpsertSchemaAssertion(ctx, in)
	if err != nil {
		if errors.Is(err, datahub.ErrAssertionCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required",
				"datahub_schema_assertion requires DataHub Cloud.")
			return
		}
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = state.ID
	plan.URN = state.URN
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *schemaAssertionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state schemaAssertionResourceModel
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

	if err := r.client.DeleteCloudAssertionWithMonitor(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *schemaAssertionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" || !strings.HasPrefix(raw, assertionURNPrefix) {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			"Expected a full DataHub assertion URN (e.g. urn:li:assertion:<uuid>).",
		)
		return
	}

	ai, err := r.client.GetAssertionByURN(ctx, raw)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if ai == nil {
		resp.Diagnostics.AddError(
			"Assertion not found",
			fmt.Sprintf("No assertion with URN %q was found in DataHub.", raw),
		)
		return
	}
	if d, bad := nonNativeAssertionError(raw, ai.Source); bad {
		resp.Diagnostics.Append(d)
		return
	}
	if ai.Schema == nil {
		resp.Diagnostics.AddError(
			"Wrong assertion type",
			fmt.Sprintf("URN %q is a %q assertion, not a schema assertion.", raw, ai.Type),
		)
		return
	}

	onSuccess, d := stringsToList(ctx, ai.OnSuccessActions)
	resp.Diagnostics.Append(d...)
	onFailure, d := stringsToList(ctx, ai.OnFailureActions)
	resp.Diagnostics.Append(d...)
	fields, d := fieldsFromInfo(ctx, ai.Schema.Fields)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := schemaAssertionResourceModel{
		ID:                 types.StringValue(ai.URN),
		URN:                types.StringValue(ai.URN),
		EntityURN:          types.StringValue(ai.EntityURN),
		Description:        nullIfEmpty(ai.Description),
		Compatibility:      types.StringValue(ai.Schema.Compatibility),
		Fields:             fields,
		OnSuccessActions:   onSuccess,
		OnFailureActions:   onFailure,
		EvaluationCron:     types.StringValue(""),
		EvaluationTimezone: types.StringValue("UTC"),
		Mode:               types.StringValue("ACTIVE"),
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// buildInput assembles the client input from a plan model.
func (r *schemaAssertionResource) buildInput(ctx context.Context, plan schemaAssertionResourceModel, assertionURN string) (datahub.SchemaAssertionInput, diag.Diagnostics) {
	var diags diag.Diagnostics
	onSuccess, d := listToStrings(ctx, plan.OnSuccessActions)
	diags.Append(d...)
	onFailure, d := listToStrings(ctx, plan.OnFailureActions)
	diags.Append(d...)
	fields, d := fieldsToInput(ctx, plan.Fields)
	diags.Append(d...)

	return datahub.SchemaAssertionInput{
		AssertionURN:       assertionURN,
		EntityURN:          plan.EntityURN.ValueString(),
		Description:        strVal(plan.Description),
		Compatibility:      plan.Compatibility.ValueString(),
		Fields:             fields,
		EvaluationCron:     plan.EvaluationCron.ValueString(),
		EvaluationTimezone: plan.EvaluationTimezone.ValueString(),
		OnSuccessActions:   onSuccess,
		OnFailureActions:   onFailure,
		Mode:               plan.Mode.ValueString(),
		ExecutorID:         strVal(plan.ExecutorID),
	}, diags
}
