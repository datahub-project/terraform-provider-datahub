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

var (
	_ resource.Resource                = &corpUserResource{}
	_ resource.ResourceWithConfigure   = &corpUserResource{}
	_ resource.ResourceWithImportState = &corpUserResource{}
)

type corpUserResource struct {
	client *datahub.Client
}

type corpUserResourceModel struct {
	ID               types.String `tfsdk:"id"`
	URN              types.String `tfsdk:"urn"`
	Username         types.String `tfsdk:"username"`
	DisplayName      types.String `tfsdk:"display_name"`
	FullName         types.String `tfsdk:"full_name"`
	Email            types.String `tfsdk:"email"`
	Title            types.String `tfsdk:"title"`
	CustomProperties types.Map    `tfsdk:"custom_properties"`
}

func NewCorpUserResource() resource.Resource {
	return &corpUserResource{}
}

func (r *corpUserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *corpUserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_corp_user"
}

func (r *corpUserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a DataHub user catalog record (`corpUser`).\n\n" +
			"This resource manages the user's profile metadata (display name, email, title). " +
			"It does **not** create login credentials -- for native-auth login, use the " +
			"`datahub_local_user_login` resource.\n\n" +
			"Uses upsert semantics: if the user entity already exists (e.g. from SSO/JIT " +
			"provisioning, metadata ingestion, or a prior `datahub_local_user_login`), this " +
			"resource adopts it and manages the profile fields going forward.\n\n" +
			"## Naming\n\n" +
			"`username` becomes the URN suffix (`urn:li:corpuser:<username>`). This matches " +
			"the convention used by the DataHub Python SDK (`datahub` CLI) and SSO/JIT " +
			"provisioning, avoiding duplicate entities.\n\n" +
			"## Deletion\n\n" +
			"Destroying this resource hard-deletes the entire user entity (profile, " +
			"credentials, group memberships, references). If the user also has a " +
			"`datahub_local_user_login` resource, that will self-remove from state on " +
			"its next refresh.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this user (e.g. `urn:li:corpuser:alice`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"username": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Username for the user. Becomes the URN suffix (`urn:li:corpuser:<username>`). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"display_name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Human-readable display name shown throughout the DataHub UI.",
			},
			"full_name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Full legal name of the user.",
			},
			"email": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Email address of the user.",
			},
			"title": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Job title of the user.",
			},
			"custom_properties": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Arbitrary key-value metadata attached to the user (the " +
					"`customProperties` field of the `corpUserInfo` aspect). Terraform owns the " +
					"complete map: keys added outside Terraform are removed on the next apply. Keys and " +
					"values must be non-empty strings, and values must not be null. Omit the attribute " +
					"entirely (do not set an empty map) to attach no custom properties.",
				Validators: []validator.Map{
					nonEmptyStringMapValidator{},
				},
			},
		},
	}
}

func (r *corpUserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan corpUserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	customProps, d := mapValToStringMap(ctx, plan.CustomProperties)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn, err := r.client.UpsertCorpUser(ctx, datahub.UpsertCorpUserInput{
		Username:         plan.Username.ValueString(),
		DisplayName:      strVal(plan.DisplayName),
		FullName:         strVal(plan.FullName),
		Email:            strVal(plan.Email),
		Title:            strVal(plan.Title),
		CustomProperties: customProps,
	})
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = types.StringValue(urn)
	plan.URN = types.StringValue(urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *corpUserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state corpUserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}

	user, err := r.client.GetUserByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if user == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	applyCorpUserToModel(user, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *corpUserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan, state corpUserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	customProps, d := mapValToStringMap(ctx, plan.CustomProperties)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.UpsertCorpUser(ctx, datahub.UpsertCorpUserInput{
		Username:         plan.Username.ValueString(),
		DisplayName:      strVal(plan.DisplayName),
		FullName:         strVal(plan.FullName),
		Email:            strVal(plan.Email),
		Title:            strVal(plan.Title),
		CustomProperties: customProps,
	})
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = state.ID
	plan.URN = state.URN
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *corpUserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state corpUserResourceModel
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

	if err := r.client.DeleteUser(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *corpUserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a username (e.g. alice) or a full URN (e.g. urn:li:corpuser:alice).")
		return
	}

	var username, urn string
	if strings.HasPrefix(raw, corpUserURNPrefix) {
		urn = raw
		username = strings.TrimPrefix(raw, corpUserURNPrefix)
	} else {
		username = raw
		urn = corpUserURNPrefix + username
	}
	if username == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a username (e.g. alice) or a full URN (e.g. urn:li:corpuser:alice).")
		return
	}

	user, err := r.client.GetUserByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if user == nil {
		resp.Diagnostics.AddError(
			"User not found",
			fmt.Sprintf("No user with URN %q was found in DataHub. Verify the username and retry.", urn),
		)
		return
	}

	state := corpUserResourceModel{
		ID:       types.StringValue(user.URN),
		URN:      types.StringValue(user.URN),
		Username: types.StringValue(user.Username),
	}
	applyCorpUserToModel(user, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func applyCorpUserToModel(user *datahub.CorpUser, m *corpUserResourceModel) {
	m.URN = types.StringValue(user.URN)
	m.ID = types.StringValue(user.URN)
	m.DisplayName = nullIfEmpty(user.DisplayName)
	m.FullName = nullIfEmpty(user.FullName)
	m.Email = nullIfEmpty(user.Email)
	m.Title = nullIfEmpty(user.Title)
	m.CustomProperties = stringMapToTfMap(user.CustomProperties)
}
