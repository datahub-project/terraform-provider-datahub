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

var (
	_ resource.Resource                = &roleAssignmentResource{}
	_ resource.ResourceWithConfigure   = &roleAssignmentResource{}
	_ resource.ResourceWithImportState = &roleAssignmentResource{}
)

type roleAssignmentResource struct {
	client *datahub.Client
}

type roleAssignmentResourceModel struct {
	ID       types.String `tfsdk:"id"`
	ActorURN types.String `tfsdk:"actor_urn"`
	RoleURN  types.String `tfsdk:"role_urn"`
}

func NewRoleAssignmentResource() resource.Resource {
	return &roleAssignmentResource{}
}

func (r *roleAssignmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	client := pd.Client
	r.client = client
}

func (r *roleAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role_assignment"
}

func (r *roleAssignmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Assigns a built-in DataHub role (`Admin`, `Editor`, or `Reader`) to an actor -- a user " +
			"or a group.\n\n" +
			"DataHub enforces **one role per actor**: assigning a new role replaces the previous one. " +
			"For that reason the actor is the natural key (the resource `id` is the actor URN), and " +
			"**exactly one `datahub_role_assignment` may target a given actor**. Defining two for the " +
			"same actor causes them to clobber each other on every apply.\n\n" +
			"Roles themselves are built-in and read-only; resolve a role URN with the `datahub_role` " +
			"data source. Deleting this resource removes the role from the actor (its roleMembership " +
			"is cleared).\n\n" +
			"## References\n\n" +
			"Prefer expression inputs: set `actor_urn` to `datahub_corp_group.<name>.urn` or " +
			"`data.datahub_corp_user.<name>.urn`, and `role_urn` to `data.datahub_role.<name>.urn`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Mirrors `actor_urn` (the natural key: one role per actor).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"actor_urn": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "URN of the actor to assign the role to: a user (`urn:li:corpuser:<username>`) or group (`urn:li:corpGroup:<id>`). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"role_urn": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "URN of the built-in role to assign (e.g. `urn:li:dataHubRole:Editor`). Can be changed in place to reassign.",
			},
		},
	}
}

func (r *roleAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan roleAssignmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	actorURN := plan.ActorURN.ValueString()
	roleURN := plan.RoleURN.ValueString()

	if err := r.client.AssignRole(ctx, roleURN, actorURN); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	// batchAssignRole silently skips actors that do not exist; confirm the
	// assignment actually landed so a non-existent actor surfaces an error
	// rather than a phantom resource.
	got, found, err := r.client.GetActorRole(ctx, actorURN)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if !found || got != roleURN {
		resp.Diagnostics.AddError(
			"Role assignment did not take effect",
			fmt.Sprintf("After assigning %q to %q, the role was not present on read back. "+
				"Verify the actor exists in DataHub (the provider does not create users).", roleURN, actorURN),
		)
		return
	}

	plan.ID = types.StringValue(actorURN)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state roleAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	actorURN := state.ActorURN.ValueString()
	roleURN, found, err := r.client.GetActorRole(ctx, actorURN)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(actorURN)
	state.RoleURN = types.StringValue(roleURN)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *roleAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan roleAssignmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	actorURN := plan.ActorURN.ValueString()
	roleURN := plan.RoleURN.ValueString()

	if err := r.client.AssignRole(ctx, roleURN, actorURN); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = types.StringValue(actorURN)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state roleAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.UnassignRole(ctx, state.ActorURN.ValueString()); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *roleAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	actorURN := strings.TrimSpace(req.ID)
	if _, err := actorEntityTypeForImport(actorURN); err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	roleURN, found, err := r.client.GetActorRole(ctx, actorURN)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Role assignment not found",
			fmt.Sprintf("Actor %q has no role assigned in DataHub.", actorURN),
		)
		return
	}

	state := roleAssignmentResourceModel{
		ID:       types.StringValue(actorURN),
		ActorURN: types.StringValue(actorURN),
		RoleURN:  types.StringValue(roleURN),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// actorEntityTypeForImport validates that the import ID is a user or group URN.
func actorEntityTypeForImport(actorURN string) (string, error) {
	switch {
	case strings.HasPrefix(actorURN, "urn:li:corpuser:"):
		return "corpuser", nil
	case strings.HasPrefix(actorURN, "urn:li:corpGroup:"):
		return "corpgroup", nil
	default:
		return "", fmt.Errorf("expected an actor URN (urn:li:corpuser:<username> or urn:li:corpGroup:<id>); got %q", actorURN)
	}
}
