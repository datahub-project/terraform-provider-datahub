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
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

const corpGroupURNPrefix = "urn:li:corpGroup:"

var (
	_ resource.Resource                = &corpGroupResource{}
	_ resource.ResourceWithConfigure   = &corpGroupResource{}
	_ resource.ResourceWithImportState = &corpGroupResource{}
)

type corpGroupResource struct {
	client *datahub.Client
}

type corpGroupResourceModel struct {
	ID          types.String `tfsdk:"id"`
	URN         types.String `tfsdk:"urn"`
	GroupID     types.String `tfsdk:"group_id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Email       types.String `tfsdk:"email"`
	Slack       types.String `tfsdk:"slack"`
}

func NewCorpGroupResource() resource.Resource {
	return &corpGroupResource{}
}

func (r *corpGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *corpGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_corp_group"
}

func (r *corpGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a native DataHub group (`corpGroup`).\n\n" +
			"Groups are collections of users used as a unit in ownership and access policies. " +
			"This resource manages **native** groups (created and owned in DataHub). It is not " +
			"intended for IdP-synced groups, whose membership and properties are owned by your " +
			"identity provider and would be overwritten on apply.\n\n" +
			"Membership is managed separately via the `datahub_corp_group_member` resource so that " +
			"users and group bindings can be composed independently.\n\n" +
			"## Naming\n\n" +
			"`group_id` becomes the URN suffix (`urn:li:corpGroup:<group_id>`). Supplying an explicit, " +
			"deterministic id avoids the random UUID that the DataHub UI assigns, and keeps the URN " +
			"stable and predictable for use as a policy actor or owner reference.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this group (e.g., `urn:li:corpGroup:data-platform`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"group_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Unique identifier for the group. Becomes the URN suffix (`urn:li:corpGroup:<group_id>`). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable display name for the group, shown throughout the DataHub UI.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the group's purpose.",
			},
			"email": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Contact email address for the group.",
			},
			"slack": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Slack channel or handle for the group (e.g., `#data-platform`).",
			},
		},
	}
}

// hasEditableProps reports whether any of the editable group properties are set.
func (m *corpGroupResourceModel) hasEditableProps() bool {
	return strVal(m.Description) != "" || strVal(m.Email) != "" || strVal(m.Slack) != ""
}

func (r *corpGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan corpGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groupID := plan.GroupID.ValueString()
	urn, err := r.client.CreateGroup(ctx, datahub.CreateGroupInput{
		ID:   groupID,
		Name: plan.Name.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	if plan.hasEditableProps() {
		if err := r.client.UpdateGroupProperties(ctx, datahub.UpdateGroupPropsInput{
			URN:         urn,
			Description: strVal(plan.Description),
			Email:       strVal(plan.Email),
			Slack:       strVal(plan.Slack),
		}); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	plan.ID = types.StringValue(urn)
	plan.URN = types.StringValue(urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *corpGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state corpGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}

	group, err := r.client.GetGroupByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if group == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	applyGroupToModel(group, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *corpGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan, state corpGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()

	if plan.Name.ValueString() != state.Name.ValueString() {
		if err := r.client.UpdateGroupName(ctx, urn, plan.Name.ValueString()); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	// Always reconcile editable properties (empty strings clear them) so values
	// removed from configuration are cleared on the server.
	if err := r.client.UpdateGroupProperties(ctx, datahub.UpdateGroupPropsInput{
		URN:         urn,
		Description: strVal(plan.Description),
		Email:       strVal(plan.Email),
		Slack:       strVal(plan.Slack),
	}); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = state.ID
	plan.URN = state.URN
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *corpGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state corpGroupResourceModel
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

	if err := r.client.DeleteGroup(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *corpGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub group URN (e.g., urn:li:corpGroup:data-platform) or a bare group ID.")
		return
	}

	var groupID, urn string
	if strings.HasPrefix(raw, corpGroupURNPrefix) {
		urn = raw
		groupID = strings.TrimPrefix(raw, corpGroupURNPrefix)
	} else {
		groupID = raw
		urn = corpGroupURNPrefix + groupID
	}
	if groupID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub group URN (e.g., urn:li:corpGroup:data-platform) or a bare group ID.")
		return
	}

	group, err := r.client.GetGroupByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if group == nil {
		resp.Diagnostics.AddError(
			"Group not found",
			fmt.Sprintf("No group with URN %q was found in DataHub. Verify the ID or URN and retry.", urn),
		)
		return
	}

	state := corpGroupResourceModel{
		ID:      types.StringValue(group.URN),
		URN:     types.StringValue(group.URN),
		GroupID: types.StringValue(group.ID),
	}
	applyGroupToModel(group, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// applyGroupToModel maps a read CorpGroup onto the model, normalizing empty
// optional fields to null so they do not show spurious drift.
func applyGroupToModel(group *datahub.CorpGroup, m *corpGroupResourceModel) {
	m.URN = types.StringValue(group.URN)
	m.ID = types.StringValue(group.URN)
	m.Name = types.StringValue(group.Name)
	m.Description = nullIfEmpty(group.Description)
	m.Email = nullIfEmpty(group.Email)
	m.Slack = nullIfEmpty(group.Slack)
}

// strVal returns the string value, or "" if the value is null/unknown.
func strVal(v types.String) string {
	if v.IsNull() || v.IsUnknown() {
		return ""
	}
	return v.ValueString()
}

// nullIfEmpty returns a null types.String for an empty input, else the value.
func nullIfEmpty(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
