// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
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

// serviceAccountIDCharset is the allowed character set for service_account_id.
// Hyphens are permitted so a UI-created service_<uuid> account can be imported
// and round-tripped.
var serviceAccountIDCharset = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// serviceAccountIDValidator rejects invalid service_account_id values at plan
// time: empty/whitespace, a leading "service_" (which would double the prefix),
// or characters outside the URN-safe set. It rejects rather than silently
// transforming, so the configured id always matches the resulting URN.
type serviceAccountIDValidator struct{}

func (v serviceAccountIDValidator) Description(_ context.Context) string {
	return "must be non-empty, URN-safe ([A-Za-z0-9._-]), and must not begin with \"service_\""
}

func (v serviceAccountIDValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v serviceAccountIDValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	id := req.ConfigValue.ValueString()
	if strings.TrimSpace(id) == "" {
		resp.Diagnostics.AddAttributeError(req.Path, "Empty service_account_id", "service_account_id must not be empty.")
		return
	}
	if strings.HasPrefix(id, "service_") {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Redundant service_ prefix",
			fmt.Sprintf("%q begins with \"service_\", but the provider adds that prefix automatically "+
				"(the URN is urn:li:corpuser:service_<service_account_id>). Drop the leading \"service_\".", id),
		)
		return
	}
	if !serviceAccountIDCharset.MatchString(id) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid service_account_id",
			fmt.Sprintf("%q contains characters outside the allowed set. Use only letters, digits, dots, hyphens, and underscores.", id),
		)
	}
}

var (
	_ resource.Resource                = &serviceAccountResource{}
	_ resource.ResourceWithConfigure   = &serviceAccountResource{}
	_ resource.ResourceWithImportState = &serviceAccountResource{}
)

type serviceAccountResource struct {
	client *datahub.Client
}

type serviceAccountResourceModel struct {
	ID               types.String `tfsdk:"id"`
	URN              types.String `tfsdk:"urn"`
	ServiceAccountID types.String `tfsdk:"service_account_id"`
	DisplayName      types.String `tfsdk:"display_name"`
	Description      types.String `tfsdk:"description"`
}

func NewServiceAccountResource() resource.Resource {
	return &serviceAccountResource{}
}

func (r *serviceAccountResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *serviceAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_account"
}

func (r *serviceAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a DataHub service account: a non-human identity for programmatic " +
			"access (CI/CD, ingestion, automation).\n\n" +
			"Requires DataHub Core >= 1.4.0 or DataHub Cloud. A service account is a `corpUser` " +
			"entity carrying a `SERVICE_ACCOUNT` subtype under a `service_` URN prefix; the feature " +
			"(including the `subTypes`-on-`corpUser` registration and the `listServiceAccounts` " +
			"query) was added in Core 1.4.0. Managing a service account requires the " +
			"`Manage Users & Groups` privilege (the `Admin` role has it).\n\n" +
			"## Naming\n\n" +
			"`service_account_id` becomes the URN suffix after the reserved prefix " +
			"(`urn:li:corpuser:service_<service_account_id>`). Supply a stable human-readable slug " +
			"(e.g. `ci-bot`), not a UUID. Do not include the `service_` prefix yourself -- the " +
			"provider adds it.\n\n" +
			"## Write path\n\n" +
			"Create and update write the `corpUserKey`, `corpUserInfo`, and `subTypes` aspects " +
			"directly via the DataHub OpenAPI v3 endpoint with the user-supplied id. The GraphQL " +
			"`createServiceAccount` mutation is not used because it generates a server-side random " +
			"UUID for the id, which is incompatible with Terraform's declarative model.\n\n" +
			"## Access tokens\n\n" +
			"This resource manages the service-account identity only. Access tokens are minted " +
			"separately (Settings -> Access Tokens, token type \"Service Account\", or the " +
			"`createAccessToken` API) and are write-once; they are not managed here.\n\n" +
			"## Scope guard\n\n" +
			"This resource refuses to manage a `corpUser` that is not a service account (missing the " +
			"`SERVICE_ACCOUNT` subtype). Importing a human user's URN fails for that reason.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this service account (e.g. `urn:li:corpuser:service_ci-bot`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"service_account_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Unique identifier for the service account. Becomes the URN suffix after the " +
					"reserved prefix (`urn:li:corpuser:service_<service_account_id>`). Use a stable " +
					"human-readable slug (e.g. `ci-bot`); do not include the `service_` prefix. " +
					"Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					serviceAccountIDValidator{},
				},
			},
			"display_name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Human-readable display name shown throughout the DataHub UI.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Description of the service account's purpose (stored as the corpUser title).",
			},
		},
	}
}

func (r *serviceAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan serviceAccountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn, err := r.client.UpsertServiceAccount(ctx, plan.ServiceAccountID.ValueString(), strVal(plan.DisplayName), strVal(plan.Description))
	if err != nil {
		if errors.Is(err, datahub.ErrServiceAccountsUnsupported) {
			resp.Diagnostics.AddError("Service accounts not supported", err.Error())
		} else {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
		}
		return
	}

	plan.ID = types.StringValue(urn)
	plan.URN = types.StringValue(urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *serviceAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state serviceAccountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}

	sa, err := r.client.GetServiceAccountByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if sa == nil {
		// Gone, or no longer a service account.
		resp.State.RemoveResource(ctx)
		return
	}

	applyServiceAccountToModel(sa, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *serviceAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan, state serviceAccountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := r.client.UpsertServiceAccount(ctx, plan.ServiceAccountID.ValueString(), strVal(plan.DisplayName), strVal(plan.Description)); err != nil {
		if errors.Is(err, datahub.ErrServiceAccountsUnsupported) {
			resp.Diagnostics.AddError("Service accounts not supported", err.Error())
		} else {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
		}
		return
	}

	plan.ID = state.ID
	plan.URN = state.URN
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *serviceAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state serviceAccountResourceModel
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

	// Reuse the corpUser hard-delete (removeUser). It does not verify the
	// SERVICE_ACCOUNT subtype server-side, but Delete only runs on a resource
	// already in state, and the subtype-guarded Read would have dropped a
	// drifted-to-non-service-account entity before Delete could target it.
	if err := r.client.DeleteUser(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *serviceAccountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a service account URN (e.g. urn:li:corpuser:service_ci-bot), a service_-prefixed username, or a bare id.")
		return
	}

	// Accept: full URN, service_<id> username, or a bare <id>.
	username := strings.TrimPrefix(raw, "urn:li:corpuser:")
	if !strings.HasPrefix(username, "service_") {
		username = "service_" + username
	}
	urn := "urn:li:corpuser:" + username

	sa, err := r.client.GetServiceAccountByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if sa == nil {
		resp.Diagnostics.AddError(
			"Service account not found",
			fmt.Sprintf("No service account with URN %q was found, or the entity exists but is not a service account "+
				"(missing the SERVICE_ACCOUNT subtype). Verify the id/URN and retry.", urn),
		)
		return
	}

	state := serviceAccountResourceModel{
		ID:  types.StringValue(sa.URN),
		URN: types.StringValue(sa.URN),
	}
	applyServiceAccountToModel(sa, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// applyServiceAccountToModel maps a read service account (corpUser) onto the
// model, deriving the bare id from the URN and normalising optional fields to
// null when empty to avoid spurious drift. description reads from the raw
// corpUserInfo title (InfoTitle) so UI edits to editable title do not shadow it.
func applyServiceAccountToModel(sa *datahub.CorpUser, m *serviceAccountResourceModel) {
	m.URN = types.StringValue(sa.URN)
	m.ID = types.StringValue(sa.URN)
	m.ServiceAccountID = types.StringValue(datahub.ServiceAccountIDFromURN(sa.URN))
	m.DisplayName = nullIfEmpty(sa.DisplayName)
	m.Description = nullIfEmpty(sa.InfoTitle)
}
