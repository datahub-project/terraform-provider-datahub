// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// hexColorRegexp validates a CSS hex colour code: "#" followed by exactly six
// hexadecimal digits (case-insensitive). E.g. #FF6B6B or #ff6b6b.
var hexColorRegexp = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// hexColorValidator is a schema.Validator that enforces the #RRGGBB format.
type hexColorValidator struct{}

func (v hexColorValidator) Description(_ context.Context) string {
	return "must be a six-digit hex colour code starting with # (e.g. #FF6B6B)"
}

func (v hexColorValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v hexColorValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	val := req.ConfigValue.ValueString()
	if !hexColorRegexp.MatchString(val) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid colour format",
			fmt.Sprintf("%q is not a valid six-digit hex colour code. Expected format: #RRGGBB (e.g. #FF6B6B).", val),
		)
	}
}

const tagURNPrefix = "urn:li:tag:"

var (
	_ resource.Resource                = &tagResource{}
	_ resource.ResourceWithConfigure   = &tagResource{}
	_ resource.ResourceWithImportState = &tagResource{}
)

type tagResource struct {
	client *datahub.Client
}

type tagResourceModel struct {
	ID          types.String `tfsdk:"id"`
	URN         types.String `tfsdk:"urn"`
	TagID       types.String `tfsdk:"tag_id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	ColorHex    types.String `tfsdk:"color_hex"`
}

func NewTagResource() resource.Resource {
	return &tagResource{}
}

func (r *tagResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *tagResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tag"
}

func (r *tagResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a DataHub tag.\n\n" +
			"Tags are flat labels applied to data assets across DataHub. This resource manages " +
			"the tag entity itself -- its name, description, and display colour -- rather than " +
			"where the tag is applied. Tag application to datasets, columns, or other assets is " +
			"per-asset enrichment and is out of scope for this provider.\n\n" +
			"## Naming\n\n" +
			"`tag_id` becomes the URN suffix (`urn:li:tag:<tag_id>`). Supplying an explicit, " +
			"deterministic id avoids the random UUID the DataHub UI assigns, and keeps the URN " +
			"stable and predictable across environments.\n\n" +
			"## Renaming\n\n" +
			"DataHub's generic `updateName` mutation does not support the Tag entity type. " +
			"Renames are performed by writing the `tagProperties` aspect directly via the " +
			"OpenAPI v3 collection endpoint; this is transparent to Terraform users.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this tag (e.g., `urn:li:tag:pii`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"tag_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Unique identifier for the tag. Becomes the URN suffix (`urn:li:tag:<tag_id>`). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable display name for the tag, shown throughout the DataHub UI.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the tag's meaning and intended usage.",
			},
			"color_hex": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Display colour for the tag badge in the DataHub UI. Must be a six-digit hex colour string including the leading `#` (e.g. `#FF6B6B`). Case-insensitive.",
				Validators: []validator.String{
					hexColorValidator{},
				},
			},
		},
	}
}

func (r *tagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan tagResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn, err := r.client.CreateTag(ctx, datahub.CreateTagInput{
		ID:          plan.TagID.ValueString(),
		Name:        plan.Name.ValueString(),
		Description: strVal(plan.Description),
	})
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	// Set color separately via the dedicated mutation (createTag does not accept it).
	if colorHex := strVal(plan.ColorHex); colorHex != "" {
		if err := r.client.SetTagColor(ctx, urn, colorHex); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", fmt.Sprintf("failed to set tag color: %s", err))
			return
		}
	}

	plan.ID = types.StringValue(urn)
	plan.URN = types.StringValue(urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *tagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state tagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}

	tag, err := r.client.GetTagByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if tag == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	applyTagToModel(tag, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *tagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan, state tagResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()

	planName := plan.Name.ValueString()
	planDesc := strVal(plan.Description)
	planColor := strVal(plan.ColorHex)
	stateColor := strVal(state.ColorHex)

	if planName != state.Name.ValueString() {
		// updateName does not support TAG entity type; write tagProperties aspect
		// directly. This call writes all three fields atomically so no other field
		// is inadvertently cleared.
		if err := r.client.WriteTagProperties(ctx, urn, planName, planDesc, planColor); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	} else {
		// Name unchanged -- apply description and colour independently.
		if planDesc != strVal(state.Description) {
			if err := r.client.UpdateEntityDescription(ctx, urn, planDesc); err != nil {
				resp.Diagnostics.AddError("DataHub API Error", err.Error())
				return
			}
		}
		if planColor != stateColor {
			if planColor != "" {
				if err := r.client.SetTagColor(ctx, urn, planColor); err != nil {
					resp.Diagnostics.AddError("DataHub API Error", err.Error())
					return
				}
			} else {
				// Color removed: write tagProperties to clear the field.
				if err := r.client.WriteTagProperties(ctx, urn, planName, planDesc, ""); err != nil {
					resp.Diagnostics.AddError("DataHub API Error", err.Error())
					return
				}
			}
		}
	}

	plan.ID = state.ID
	plan.URN = state.URN
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *tagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state tagResourceModel
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

	if err := r.client.DeleteTag(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *tagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub tag URN (e.g., urn:li:tag:pii) or a bare tag ID.")
		return
	}

	var tagID, urn string
	if strings.HasPrefix(raw, tagURNPrefix) {
		urn = raw
		tagID = strings.TrimPrefix(raw, tagURNPrefix)
	} else {
		tagID = raw
		urn = tagURNPrefix + tagID
	}
	if tagID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub tag URN (e.g., urn:li:tag:pii) or a bare tag ID.")
		return
	}

	tag, err := r.client.GetTagByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if tag == nil {
		resp.Diagnostics.AddError(
			"Tag not found",
			fmt.Sprintf("No tag with URN %q was found in DataHub. Verify the ID or URN and retry.", urn),
		)
		return
	}

	state := tagResourceModel{
		ID:    types.StringValue(tag.URN),
		URN:   types.StringValue(tag.URN),
		TagID: types.StringValue(tag.ID),
	}
	applyTagToModel(tag, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// applyTagToModel maps a read Tag onto the model, normalising empty optional
// fields to null so they do not show spurious drift.
func applyTagToModel(tag *datahub.Tag, m *tagResourceModel) {
	m.URN = types.StringValue(tag.URN)
	m.ID = types.StringValue(tag.URN)
	m.Name = types.StringValue(tag.Name)
	m.Description = nullIfEmpty(tag.Description)
	m.ColorHex = nullIfEmpty(tag.ColorHex)
}
