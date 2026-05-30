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

// memberIDSeparator joins group and user URNs into the composite resource id.
const memberIDSeparator = "|"

var (
	_ resource.Resource                = &corpGroupMemberResource{}
	_ resource.ResourceWithConfigure   = &corpGroupMemberResource{}
	_ resource.ResourceWithImportState = &corpGroupMemberResource{}
)

type corpGroupMemberResource struct {
	client *datahub.Client
}

type corpGroupMemberResourceModel struct {
	ID       types.String `tfsdk:"id"`
	GroupURN types.String `tfsdk:"group_urn"`
	UserURN  types.String `tfsdk:"user_urn"`
}

func NewCorpGroupMemberResource() resource.Resource {
	return &corpGroupMemberResource{}
}

func (r *corpGroupMemberResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *corpGroupMemberResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_corp_group_member"
}

func (r *corpGroupMemberResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Manages a single membership edge: one user's membership in one native DataHub group.\n\n" +
			"Following the HashiCorp idiom, each membership is its own resource rather than a list on " +
			"the group. This lets group definitions and membership bindings be composed and owned " +
			"independently, and lets memberships be added or removed without rewriting the whole group.\n\n" +
			"Membership is stored on the user (the `nativeGroupMembership` aspect), so this resource " +
			"targets **native** groups created in DataHub. It is not intended for IdP-synced groups, " +
			"whose membership is owned by your identity provider.\n\n" +
			"## References\n\n" +
			"Prefer expression inputs so Terraform orders operations correctly: set `group_urn` to " +
			"`datahub_corp_group.<name>.urn` and `user_urn` to `data.datahub_corp_user.<name>.urn`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Composite identifier: `<group_urn>|<user_urn>`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"group_urn": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Full URN of the group (e.g., `urn:li:corpGroup:data-platform`). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"user_urn": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Full URN of the user (e.g., `urn:li:corpuser:alice`). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func memberID(groupURN, userURN string) string {
	return groupURN + memberIDSeparator + userURN
}

func (r *corpGroupMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan corpGroupMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groupURN := plan.GroupURN.ValueString()
	userURN := plan.UserURN.ValueString()

	if err := r.client.AddGroupMember(ctx, groupURN, userURN); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = types.StringValue(memberID(groupURN, userURN))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *corpGroupMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state corpGroupMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groupURN := state.GroupURN.ValueString()
	userURN := state.UserURN.ValueString()

	exists, err := r.client.GroupMemberExists(ctx, groupURN, userURN)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(memberID(groupURN, userURN))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is unreachable: both attributes force replacement. Implemented to
// satisfy the resource.Resource interface.
func (r *corpGroupMemberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan corpGroupMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *corpGroupMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state corpGroupMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.RemoveGroupMember(ctx, state.GroupURN.ValueString(), state.UserURN.ValueString()); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *corpGroupMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	groupURN, userURN, ok := strings.Cut(raw, memberIDSeparator)
	groupURN = strings.TrimSpace(groupURN)
	userURN = strings.TrimSpace(userURN)
	if !ok || groupURN == "" || userURN == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			"Expected a composite ID of the form \"<group_urn>|<user_urn>\" "+
				"(e.g., urn:li:corpGroup:data-platform|urn:li:corpuser:alice).",
		)
		return
	}

	exists, err := r.client.GroupMemberExists(ctx, groupURN, userURN)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if !exists {
		resp.Diagnostics.AddError(
			"Membership not found",
			fmt.Sprintf("User %q is not a member of group %q in DataHub.", userURN, groupURN),
		)
		return
	}

	state := corpGroupMemberResourceModel{
		ID:       types.StringValue(memberID(groupURN, userURN)),
		GroupURN: types.StringValue(groupURN),
		UserURN:  types.StringValue(userURN),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
