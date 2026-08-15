// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ resource.Resource                = &oauthAuthorizationServerResource{}
	_ resource.ResourceWithConfigure   = &oauthAuthorizationServerResource{}
	_ resource.ResourceWithImportState = &oauthAuthorizationServerResource{}
)

type oauthAuthorizationServerResource struct {
	client *datahub.Client
}

// oauthAuthorizationServerResourceModel is the Terraform state model for
// datahub_oauth_authorization_server.
//
// client_secret_wo is WriteOnly (never persisted to state); the server stores
// it encrypted as a separate dataHubSecret entity and only ever exposes its
// presence (has_client_secret) and the referencing URN (client_secret_urn).
type oauthAuthorizationServerResourceModel struct {
	ID                    types.String `tfsdk:"id"`
	URN                   types.String `tfsdk:"urn"`
	ServerID              types.String `tfsdk:"server_id"`
	DisplayName           types.String `tfsdk:"display_name"`
	Description           types.String `tfsdk:"description"`
	ClientID              types.String `tfsdk:"client_id"`
	ClientSecretWO        types.String `tfsdk:"client_secret_wo"`
	ClientSecretWOVersion types.Int64  `tfsdk:"client_secret_wo_version"`
	HasClientSecret       types.Bool   `tfsdk:"has_client_secret"`
	ClientSecretURN       types.String `tfsdk:"client_secret_urn"`
	AuthorizationURL      types.String `tfsdk:"authorization_url"`
	TokenURL              types.String `tfsdk:"token_url"`
	Scopes                types.List   `tfsdk:"scopes"`
	TokenAuthMethod       types.String `tfsdk:"token_auth_method"`
	AuthLocation          types.String `tfsdk:"auth_location"`
	AuthHeaderName        types.String `tfsdk:"auth_header_name"`
	AuthScheme            types.String `tfsdk:"auth_scheme"`
	AuthQueryParam        types.String `tfsdk:"auth_query_param"`
	AdditionalTokenParams types.Map    `tfsdk:"additional_token_params"`
	AdditionalAuthParams  types.Map    `tfsdk:"additional_auth_params"`
}

func NewOAuthAuthorizationServerResource() resource.Resource {
	return &oauthAuthorizationServerResource{}
}

func (r *oauthAuthorizationServerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	r.client = pd.Client
}

func (r *oauthAuthorizationServerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_oauth_authorization_server"
}

func (r *oauthAuthorizationServerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: cloudOnlyBadge +
			"Creates and manages an OAuth Authorization Server: an outbound OAuth client " +
			"configuration DataHub uses to obtain tokens from an external identity provider " +
			"when calling an external API. Its primary consumer is the Ask DataHub AI plugin " +
			"registry, where a plugin's `USER_OAUTH` mode references a server managed by this " +
			"resource.\n\n" +
			"This is a DataHub Cloud capability; DataHub Cloud upgrades on its own cadence, so a " +
			"Cloud release may occasionally affect this resource. Fixes are handled in the " +
			"provider - pin the provider version for stability and upgrade to pick up fixes. " +
			"Both mutations behind this resource require the `MANAGE_CONNECTIONS` privilege.\n\n" +
			"## Client secret handling\n\n" +
			"`client_secret_wo` is **WriteOnly**: Terraform sends it to DataHub on apply but never " +
			"persists it to state. DataHub encrypts the value server-side and stores it as a " +
			"separate `dataHubSecret` entity; only its presence (`has_client_secret`) and the " +
			"referencing URN (`client_secret_urn`) are readable. " +
			"**Requires Terraform CLI 1.11 or later** (WriteOnly attribute support).\n\n" +
			"### Rotation is in-place, unlike `datahub_secret`\n\n" +
			"To rotate the secret, update `client_secret_wo` and increment " +
			"`client_secret_wo_version`. Unlike `datahub_secret` and `datahub_connection`, the " +
			"version bump triggers an **in-place update, not a replacement**: destroying this " +
			"resource cascades into every AI plugin referencing it (DataHub flips their auth to " +
			"`NONE`), so a replacement for a routine rotation would silently break those plugins. " +
			"The upsert supports in-place secret replacement, and the provider uses it.\n\n" +
			"To **clear** the secret, remove `client_secret_wo` from config and increment " +
			"`client_secret_wo_version`; the version says \"act on the secret now\", the attribute " +
			"says what to act with. When the version is unchanged, the stored secret is preserved " +
			"and never re-sent.\n\n" +
			"### Rotation leaves an orphan trail\n\n" +
			"Every rotation makes DataHub create a **new** encrypted `dataHubSecret` entity; " +
			"nothing deletes the previous one. The orphaned secrets are inert but permanent - the " +
			"provider cannot clean them up. `client_secret_urn` changing between reads is the " +
			"visible sign of a rotation (yours or someone else's).\n\n" +
			"### Drift detection is partial\n\n" +
			"Every non-secret attribute is fully drift-detected via the strongly-consistent read " +
			"path. Secret **presence** is drift-detected: if an admin clears the secret in the UI, " +
			"`has_client_secret` flips to `false` on the next plan - bump " +
			"`client_secret_wo_version` to re-push it. Secret **value** drift is undetectable by " +
			"design: an out-of-band rotation leaves `has_client_secret = true` and Terraform " +
			"reports no change until you bump the version.\n\n" +
			"## Deletion cascades\n\n" +
			"Destroying this resource makes DataHub rewrite every AI plugin that references it: " +
			"each plugin's auth type is set to `NONE` and its OAuth configuration is dropped, and " +
			"per-user OAuth connections for those plugins are removed. Deletion is a **hard " +
			"delete**. Conversely, deleting the last AI plugin that references this server makes " +
			"DataHub delete the server too; the provider tolerates that (`Read` removes the " +
			"resource from state, `Delete` treats not-found as success), but declare the " +
			"dependency (`server_urn = datahub_oauth_authorization_server.x.urn`) so " +
			"`terraform destroy` orders the two correctly.\n\n" +
			"## Import\n\n" +
			"Import by URN or bare `server_id`. Unlike `datahub_secret`, an imported server can be " +
			"updated **without re-supplying the secret**: leave `client_secret_wo` and " +
			"`client_secret_wo_version` unset and the stored secret is untouched " +
			"(`has_client_secret = true` confirms it is there). To rotate after import, set both " +
			"the value and the version in the same change.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this server (e.g. `urn:li:oauthAuthorizationServer:snowflake-oauth`). Reference this from AI plugin configuration.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"server_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Unique identifier for this server. Becomes the URN suffix " +
					"(`urn:li:oauthAuthorizationServer:<server_id>`). The provider always sends an explicit " +
					"id so the URN is deterministic (omitting it server-side mints a random UUID). " +
					"Changing this forces a new resource - note the deletion cascade above.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					urnIDValidator{attribute: "server_id"},
				},
			},
			"display_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable display name shown in the DataHub UI (e.g. `Snowflake via Okta`).",
				Validators:          []validator.String{nonEmptyString()},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of what this authorization server provides access to. Omit to clear.",
				Validators:          []validator.String{nonEmptyString()},
			},
			"client_id": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "OAuth client ID. DataHub treats this as public and returns it on read, so it is " +
					"stored in state (and fully drift-detected) - it is marked sensitive only to keep it out of " +
					"plan output. Omit to clear.",
				Validators: []validator.String{nonEmptyString()},
			},
			"client_secret_wo": schema.StringAttribute{
				Optional:  true,
				WriteOnly: true,
				Sensitive: true,
				MarkdownDescription: "OAuth client secret. **WriteOnly**: sent to DataHub (which encrypts it " +
					"server-side) and never stored in Terraform state. Only acted on when " +
					"`client_secret_wo_version` changes (plus the initial create). **Requires Terraform CLI 1.11+.**",
			},
			"client_secret_wo_version": schema.Int64Attribute{
				Optional: true,
				MarkdownDescription: "Rotation counter for `client_secret_wo`. Increment it (e.g. `1` -> `2`) to act on " +
					"the secret: with `client_secret_wo` set, the secret is replaced **in place** (no resource " +
					"replacement, unlike `datahub_secret`); with `client_secret_wo` unset, the stored secret is " +
					"cleared. The integer itself is arbitrary; only changes to it matter.",
			},
			"has_client_secret": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether a client secret is currently configured server-side. The only " +
					"value-adjacent drift signal: flips to `false` if the secret is cleared outside Terraform.",
			},
			"client_secret_urn": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "URN of the `dataHubSecret` entity holding the encrypted client secret. Changes on " +
					"every rotation (each rotation mints a new secret entity and orphans the previous one). " +
					"Informational; not used for drift detection.",
			},
			"authorization_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "OAuth authorization endpoint URL (e.g. `https://accounts.google.com/o/oauth2/v2/auth`). Omit to clear.",
				Validators:          []validator.String{nonEmptyString()},
			},
			"token_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "OAuth token endpoint URL (e.g. `https://oauth2.googleapis.com/token`). Omit to clear.",
				Validators:          []validator.String{nonEmptyString()},
			},
			"scopes": schema.ListAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Default OAuth scopes to request. The provider owns the complete list: scopes added outside Terraform are removed on the next apply. Omit to clear (do not set `[]`).",
				Validators:          []validator.List{nonEmptyStringListValidator{}},
			},
			"token_auth_method": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("POST_BODY"),
				MarkdownDescription: "How DataHub authenticates at the token endpoint: `BASIC` (client_id:client_secret in the Authorization header), `POST_BODY` (form parameters), `NONE` (public client), or `CUSTOM` (uses `auth_scheme`). Defaults to `POST_BODY`, matching the server.",
				Validators:          []validator.String{enumString("BASIC", "POST_BODY", "NONE", "CUSTOM")},
			},
			"auth_location": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("HEADER"),
				MarkdownDescription: "Where the obtained credential is injected in API requests: `HEADER` or `QUERY_PARAM`. Defaults to `HEADER`, matching the server.",
				Validators:          []validator.String{enumString("HEADER", "QUERY_PARAM")},
			},
			"auth_header_name": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("Authorization"),
				MarkdownDescription: "Header name for credential injection when `auth_location = HEADER`. Defaults to `Authorization`, matching the server.",
				Validators:          []validator.String{nonEmptyString()},
			},
			"auth_scheme": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("Bearer"),
				MarkdownDescription: "Scheme prefix before the token (`Authorization: <auth_scheme> <token>`). Defaults to `Bearer`, matching the server. Set to `\"\"` explicitly for a raw token with no prefix (e.g. `X-API-Key: <token>`).",
			},
			"auth_query_param": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Query parameter name for credential injection when `auth_location = QUERY_PARAM` (e.g. `access_token`). Omit to clear.",
				Validators:          []validator.String{nonEmptyString()},
			},
			"additional_token_params": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Additional parameters for token requests (e.g. an Auth0 `audience` or Azure AD `resource`). The provider owns the complete map. Omit to clear (do not set `{}`).",
				Validators:          []validator.Map{nonEmptyStringMapValidator{}},
			},
			"additional_auth_params": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Additional parameters for the authorization URL (e.g. `prompt = \"consent\"`). The provider owns the complete map. Omit to clear (do not set `{}`).",
				Validators:          []validator.Map{nonEmptyStringMapValidator{}},
			},
		},
	}
}

// buildUpsertInput assembles the full mutation input from the plan. Runs at
// apply time, so every plan value is resolved (never unknown). The secret
// action is decided by the caller (Create vs Update have different rules).
func buildUpsertInput(ctx context.Context, plan *oauthAuthorizationServerResourceModel, action datahub.ClientSecretAction, secretValue string) (datahub.UpsertOAuthAuthorizationServerInput, error) {
	in := datahub.UpsertOAuthAuthorizationServerInput{
		ID:                 plan.ServerID.ValueString(),
		DisplayName:        plan.DisplayName.ValueString(),
		ClientID:           plan.ClientID.ValueString(),
		ClientSecretAction: action,
		ClientSecretValue:  secretValue,
		AuthorizationURL:   plan.AuthorizationURL.ValueString(),
		TokenURL:           plan.TokenURL.ValueString(),
		TokenAuthMethod:    plan.TokenAuthMethod.ValueString(),
		AuthLocation:       plan.AuthLocation.ValueString(),
		AuthHeaderName:     plan.AuthHeaderName.ValueString(),
	}

	if !plan.Description.IsNull() {
		in.Description = plan.Description.ValueStringPointer()
	}
	// auth_scheme carries a schema default, so the plan value is never null;
	// send it verbatim ("" is meaningful: raw token, no scheme prefix).
	if !plan.AuthScheme.IsNull() {
		in.AuthScheme = plan.AuthScheme.ValueStringPointer()
	}
	if !plan.AuthQueryParam.IsNull() {
		in.AuthQueryParam = plan.AuthQueryParam.ValueStringPointer()
	}
	if !plan.Scopes.IsNull() {
		var scopes []string
		if diags := plan.Scopes.ElementsAs(ctx, &scopes, false); diags.HasError() {
			return in, fmt.Errorf("reading scopes: %v", diags.Errors())
		}
		in.Scopes = scopes
	}
	if !plan.AdditionalTokenParams.IsNull() {
		params := map[string]string{}
		if diags := plan.AdditionalTokenParams.ElementsAs(ctx, &params, false); diags.HasError() {
			return in, fmt.Errorf("reading additional_token_params: %v", diags.Errors())
		}
		in.AdditionalTokenParams = params
	}
	if !plan.AdditionalAuthParams.IsNull() {
		params := map[string]string{}
		if diags := plan.AdditionalAuthParams.ElementsAs(ctx, &params, false); diags.HasError() {
			return in, fmt.Errorf("reading additional_auth_params: %v", diags.Errors())
		}
		in.AdditionalAuthParams = params
	}
	return in, nil
}

// setComputedFromServer copies the server-derived computed attributes into the
// model. Non-computed attributes are NOT touched: after Create/Update the
// framework requires them to match the plan, and the plan is authoritative.
func setComputedFromServer(m *oauthAuthorizationServerResourceModel, got *datahub.OAuthAuthorizationServer) {
	m.URN = types.StringValue(got.URN)
	m.ID = types.StringValue(got.URN)
	m.HasClientSecret = types.BoolValue(got.HasClientSecret())
	if got.ClientSecretURN != "" {
		m.ClientSecretURN = types.StringValue(got.ClientSecretURN)
	} else {
		m.ClientSecretURN = types.StringNull()
	}
}

func (r *oauthAuthorizationServerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan oauthAuthorizationServerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// WriteOnly attributes are null in the plan; read them from the request
	// config.
	var config oauthAuthorizationServerResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// On create there is no stored secret to preserve or clear: send the
	// configured value when present, an explicit null otherwise.
	action := datahub.ClientSecretPreserve
	secretValue := ""
	if !config.ClientSecretWO.IsNull() {
		action = datahub.ClientSecretSet
		secretValue = config.ClientSecretWO.ValueString()
	}

	in, err := buildUpsertInput(ctx, &plan, action, secretValue)
	if err != nil {
		resp.Diagnostics.AddError("Config serialization error", err.Error())
		return
	}

	urn, err := r.client.UpsertOAuthAuthorizationServer(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	// Read back via the strongly-consistent entity endpoint: verifies the
	// write persisted (silent-no-op guard) and recovers the computed fields.
	got, err := r.client.GetOAuthAuthorizationServerByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", fmt.Sprintf("verifying OAuth authorization server write: %s", err))
		return
	}
	if got == nil {
		resp.Diagnostics.AddError(
			"DataHub API Error",
			fmt.Sprintf("DataHub accepted the OAuth authorization server write but %s was not readable afterwards", urn),
		)
		return
	}

	setComputedFromServer(&plan, got)
	// client_secret_wo is WriteOnly -- the framework nullifies it in state
	// automatically.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *oauthAuthorizationServerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state oauthAuthorizationServerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}

	got, err := r.client.GetOAuthAuthorizationServerByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if got == nil {
		// Gone - possibly deleted by deleteAiPlugin's cascade (deleting the
		// last referencing plugin deletes an unshared server). Removing from
		// state lets the next apply recreate it cleanly.
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(populateOAuthServerFromServer(ctx, &state, got)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// populateOAuthServerFromServer maps the server read-shape onto the model,
// normalising the values the provider's own writes produce:
//   - "" for clientId/authorizationUrl/tokenUrl/description means "not set"
//     (the provider writes "" to clear the preserve-on-null fields, and the
//     nonEmptyString validators keep "" out of user configs)
//   - an empty scopes list / params map reads back as null (the provider
//     clears with an explicit empty write, and validators forbid configured
//     empties)
//   - auth_scheme keeps "" as a value (raw-token injection) and maps only a
//     truly absent field to null
func populateOAuthServerFromServer(ctx context.Context, m *oauthAuthorizationServerResourceModel, got *datahub.OAuthAuthorizationServer) diag.Diagnostics {
	var diags diag.Diagnostics

	m.URN = types.StringValue(got.URN)
	m.ID = types.StringValue(got.URN)
	m.ServerID = types.StringValue(got.ID)
	m.DisplayName = types.StringValue(got.DisplayName)

	desc := ""
	if got.Description != nil {
		desc = *got.Description
	}
	m.Description = optionalString(desc)
	m.ClientID = optionalString(got.ClientID)
	m.AuthorizationURL = optionalString(got.AuthorizationURL)
	m.TokenURL = optionalString(got.TokenURL)
	qp := ""
	if got.AuthQueryParam != nil {
		qp = *got.AuthQueryParam
	}
	m.AuthQueryParam = optionalString(qp)

	m.TokenAuthMethod = optionalString(got.TokenAuthMethod)
	m.AuthLocation = optionalString(got.AuthLocation)
	m.AuthHeaderName = optionalString(got.AuthHeaderName)
	if got.AuthScheme != nil {
		m.AuthScheme = types.StringValue(*got.AuthScheme)
	} else {
		m.AuthScheme = types.StringNull()
	}

	scopes, d := optionalStringList(ctx, got.Scopes)
	diags.Append(d...)
	m.Scopes = scopes

	m.AdditionalTokenParams = optionalStringMap(ctx, got.AdditionalTokenParams, &diags)
	m.AdditionalAuthParams = optionalStringMap(ctx, got.AdditionalAuthParams, &diags)

	m.HasClientSecret = types.BoolValue(got.HasClientSecret())
	if got.ClientSecretURN != "" {
		m.ClientSecretURN = types.StringValue(got.ClientSecretURN)
	} else {
		m.ClientSecretURN = types.StringNull()
	}
	// client_secret_wo is WriteOnly (always null in state);
	// client_secret_wo_version keeps its state value untouched.
	return diags
}

// optionalStringMap maps an empty or nil map to null, mirroring
// optionalString/optionalStringList for map-typed attributes.
func optionalStringMap(ctx context.Context, in map[string]string, diags *diag.Diagnostics) types.Map {
	if len(in) == 0 {
		return types.MapNull(types.StringType)
	}
	mapVal, d := types.MapValueFrom(ctx, types.StringType, in)
	diags.Append(d...)
	return mapVal
}

func (r *oauthAuthorizationServerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan oauthAuthorizationServerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state oauthAuthorizationServerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// WriteOnly attributes are null in the plan; read them from the request
	// config.
	var config oauthAuthorizationServerResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The secret is acted on ONLY when the version changed. Sending the value
	// on every update would mint (and orphan) a new dataHubSecret entity per
	// unrelated change, since every non-empty write creates a new secret.
	// A version bump with client_secret_wo unset clears the stored secret.
	action := datahub.ClientSecretPreserve
	secretValue := ""
	if !plan.ClientSecretWOVersion.Equal(state.ClientSecretWOVersion) {
		if !config.ClientSecretWO.IsNull() {
			action = datahub.ClientSecretSet
			secretValue = config.ClientSecretWO.ValueString()
		} else {
			action = datahub.ClientSecretClear
		}
	}

	in, err := buildUpsertInput(ctx, &plan, action, secretValue)
	if err != nil {
		resp.Diagnostics.AddError("Config serialization error", err.Error())
		return
	}

	urn, err := r.client.UpsertOAuthAuthorizationServer(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	got, err := r.client.GetOAuthAuthorizationServerByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", fmt.Sprintf("verifying OAuth authorization server write: %s", err))
		return
	}
	if got == nil {
		resp.Diagnostics.AddError(
			"DataHub API Error",
			fmt.Sprintf("DataHub accepted the OAuth authorization server write but %s was not readable afterwards", urn),
		)
		return
	}

	setComputedFromServer(&plan, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *oauthAuthorizationServerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state oauthAuthorizationServerResourceModel
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

	// Not-found is success: deleteAiPlugin's cascade may have deleted the
	// server already (it removes an unshared server when the last referencing
	// plugin is deleted), and the client tolerates that.
	if err := r.client.DeleteOAuthAuthorizationServer(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *oauthAuthorizationServerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			"Expected an OAuth authorization server URN (e.g. urn:li:oauthAuthorizationServer:snowflake-oauth) or a bare server ID.",
		)
		return
	}

	urn := raw
	if !strings.HasPrefix(raw, datahub.OAuthAuthorizationServerURNPrefix) {
		urn = datahub.OAuthAuthorizationServerURNPrefix + raw
	}

	got, err := r.client.GetOAuthAuthorizationServerByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if got == nil {
		resp.Diagnostics.AddError(
			"OAuth authorization server not found",
			fmt.Sprintf("No OAuth authorization server with URN %q was found in DataHub. Verify the ID or URN and retry.", urn),
		)
		return
	}

	var state oauthAuthorizationServerResourceModel
	resp.Diagnostics.Append(populateOAuthServerFromServer(ctx, &state, got)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The secret is neither readable nor needed: a null client_secret_wo with
	// a null version preserves the stored secret on every subsequent apply.
	state.ClientSecretWO = types.StringNull()
	state.ClientSecretWOVersion = types.Int64Null()

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
