// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ resource.Resource                = &localUserLoginResource{}
	_ resource.ResourceWithConfigure   = &localUserLoginResource{}
	_ resource.ResourceWithImportState = &localUserLoginResource{}
)

type localUserLoginResource struct {
	client *datahub.Client
}

type localUserLoginResourceModel struct {
	ID                     types.String `tfsdk:"id"`
	UserURN                types.String `tfsdk:"user_urn"`
	Username               types.String `tfsdk:"username"`
	FullName               types.String `tfsdk:"full_name"`
	Email                  types.String `tfsdk:"email"`
	Title                  types.String `tfsdk:"title"`
	InitialPassword        types.String `tfsdk:"initial_password"`
	InitialPasswordVersion types.Int64  `tfsdk:"initial_password_wo_version"`
	PasswordResetURL       types.String `tfsdk:"password_reset_url"`
}

func NewLocalUserLoginResource() resource.Resource {
	return &localUserLoginResource{}
}

func (r *localUserLoginResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *localUserLoginResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_local_user_login"
}

func (r *localUserLoginResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates a native-auth login for a DataHub user.\n\n" +
			"This resource provisions a user entity with local credentials (username + password) " +
			"via the DataHub sign-up flow. It is separate from `datahub_corp_user` (which manages " +
			"the catalog profile) because the credential lifecycle is fundamentally different: " +
			"different API path, different auth requirements, one-way ratchet (credentials cannot " +
			"be removed without deleting the user entity).\n\n" +
			"**When `initial_password` is omitted** (recommended), the provider generates a random " +
			"throwaway password, signs up the user, then generates a single-use password reset link " +
			"(24h TTL) exposed as `password_reset_url`. Send this link to the user so they can set " +
			"their own password. DataHub has no server-side email; distribution is your responsibility.\n\n" +
			"**When `initial_password` is set**, the provider uses it directly and does not generate " +
			"a reset link. Use this for automation or test fixtures where the caller controls the " +
			"credential.\n\n" +
			"## Deletion\n\n" +
			"**Warning:** destroying this resource hard-deletes the entire user entity (profile, " +
			"credentials, group memberships, references) -- not just the credentials. This is a " +
			"DataHub API limitation (there is no endpoint to remove only credentials). If " +
			"`datahub_corp_user` also exists for the same username, it will self-remove from state " +
			"on its next refresh (404).\n\n" +
			"## Two-resource pattern\n\n" +
			"When using both resources for the same user, reference the login's `username` output " +
			"from the corp_user resource to ensure correct ordering:\n\n" +
			"```hcl\n" +
			"resource \"datahub_local_user_login\" \"bob\" {\n" +
			"  username  = \"bob\"\n" +
			"  full_name = \"Bob Jones\"\n" +
			"  email     = \"bob@example.com\"\n" +
			"}\n\n" +
			"resource \"datahub_corp_user\" \"bob\" {\n" +
			"  username = datahub_local_user_login.bob.username\n" +
			"  title    = \"Analytics Engineer\"\n" +
			"}\n" +
			"```\n\n" +
			"On OSS DataHub, the login resource **must** be created before `datahub_corp_user` " +
			"(the sign-up endpoint rejects pre-existing entities). On DataHub Cloud, either order " +
			"works. The reference pattern above is safe on both.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"user_urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this user (e.g. `urn:li:corpuser:bob`).",
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
			"full_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Full name passed to the sign-up endpoint. Changing this forces a new resource (profile updates go through `datahub_corp_user`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"email": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Email address passed to the sign-up endpoint. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"title": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Job title passed to the sign-up endpoint. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"initial_password": schema.StringAttribute{
				Optional:  true,
				WriteOnly: true,
				Sensitive: true,
				MarkdownDescription: "Initial password for the user. When omitted, the provider generates a random " +
					"throwaway password and exposes a `password_reset_url` instead. " +
					"**Requires Terraform CLI 1.11+.** Not stored in state.",
			},
			"initial_password_wo_version": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Rotation counter for `initial_password`. Increment this to force replacement and re-run the sign-up flow with the new password.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"password_reset_url": schema.StringAttribute{
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "Single-use password reset URL (24h TTL). Populated only when `initial_password` is omitted. Send this to the user so they can set their own password. Null after import or when `initial_password` is set.",
			},
		},
	}
}

func (r *localUserLoginResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan localUserLoginResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// WriteOnly attributes come from Config, not Plan.
	var config localUserLoginResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	username := plan.Username.ValueString()
	userURN := corpUserURNPrefix + username

	password := strVal(config.InitialPassword)
	passwordWasProvided := password != ""
	if !passwordWasProvided {
		randBytes := make([]byte, 48)
		if _, err := rand.Read(randBytes); err != nil {
			resp.Diagnostics.AddError("Failed to generate random password", err.Error())
			return
		}
		password = base64.RawURLEncoding.EncodeToString(randBytes)
	}

	// Step 1: obtain invite token.
	inviteToken, err := r.client.GetInviteToken(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to get invite token", err.Error())
		return
	}

	// Step 2: sign up.
	title := strVal(plan.Title)
	if title == "" {
		title = "Other"
	}
	if err := r.client.SignUp(ctx, datahub.SignUpInput{
		UserURN:     userURN,
		FullName:    plan.FullName.ValueString(),
		Email:       plan.Email.ValueString(),
		Password:    password,
		Title:       title,
		InviteToken: inviteToken,
	}); err != nil {
		resp.Diagnostics.AddError("Sign-up failed", err.Error())
		return
	}

	// Step 3: regenerate invite token to close the security window.
	if _, err := r.client.CreateInviteToken(ctx); err != nil {
		tflog.Warn(ctx, "Failed to regenerate invite token after sign-up; the used token remains valid", map[string]any{"error": err.Error()})
	}

	// Step 4: if no initial_password was provided, generate a reset token.
	var resetURL string
	if !passwordWasProvided {
		resetToken, err := r.client.CreateNativeUserResetToken(ctx, userURN)
		if err != nil {
			resp.Diagnostics.AddError("Failed to create password reset token",
				"The user was created but the password reset token could not be generated. "+
					"Generate one manually via the DataHub Admin UI. Error: "+err.Error())
			return
		}
		resetURL = r.client.FrontendURL() + "/resetCredentials?reset_token=" + resetToken
	}

	// Step 5: read back to confirm.
	user, err := r.client.GetUserByURN(ctx, userURN)
	if err != nil {
		resp.Diagnostics.AddError("Read-back failed after sign-up", err.Error())
		return
	}
	if user == nil {
		resp.Diagnostics.AddError("User not found after sign-up",
			fmt.Sprintf("The sign-up for %q appeared to succeed but the user entity was not found on read-back.", username))
		return
	}

	plan.ID = types.StringValue(userURN)
	plan.UserURN = types.StringValue(userURN)
	if resetURL != "" {
		plan.PasswordResetURL = types.StringValue(resetURL)
	} else {
		plan.PasswordResetURL = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *localUserLoginResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state localUserLoginResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.UserURN.ValueString()
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

	// Only verify entity existence. Credentials are not readable.
	// password_reset_url stays as-is from state (one-time output from Create).
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *localUserLoginResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Update not supported",
		"All attributes on datahub_local_user_login force replacement. This method should not be called.",
	)
}

func (r *localUserLoginResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state localUserLoginResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.UserURN.ValueString()
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

func (r *localUserLoginResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a username (e.g. bob) or a full URN (e.g. urn:li:corpuser:bob).")
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
		resp.Diagnostics.AddError("Invalid import ID", "Expected a username (e.g. bob) or a full URN (e.g. urn:li:corpuser:bob).")
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

	state := localUserLoginResourceModel{
		ID:               types.StringValue(urn),
		UserURN:          types.StringValue(urn),
		Username:         types.StringValue(user.Username),
		FullName:         types.StringValue(user.FullName),
		Email:            types.StringValue(user.Email),
		Title:            nullIfEmpty(user.Title),
		PasswordResetURL: types.StringNull(),
	}
	resp.Diagnostics.AddWarning(
		"Imported resource may be replaced on next apply",
		"After import, initial_password is not available and password_reset_url is null. "+
			"The next apply may detect differences in full_name, email, or title and force a replacement.",
	)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
