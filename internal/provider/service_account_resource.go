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
	_ resource.ResourceWithModifyPlan  = &serviceAccountResource{}
)

type serviceAccountResource struct {
	pd       *providerData
	client   *datahub.Client
	defaults entityDefaults
}

type serviceAccountResourceModel struct {
	ID                           types.String `tfsdk:"id"`
	URN                          types.String `tfsdk:"urn"`
	ServiceAccountID             types.String `tfsdk:"service_account_id"`
	DisplayName                  types.String `tfsdk:"display_name"`
	Description                  types.String `tfsdk:"description"`
	CustomProperties             types.Map    `tfsdk:"custom_properties"`
	CustomPropertiesAll          types.Map    `tfsdk:"custom_properties_all"`
	TagsAll                      types.Set    `tfsdk:"tags_all"`
	StructuredPropertiesDefaults types.Map    `tfsdk:"structured_properties_defaults"`
}

func NewServiceAccountResource() resource.Resource {
	return &serviceAccountResource{}
}

func (r *serviceAccountResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	r.pd = pd
	r.client = pd.Client
	r.defaults = pd.defaults
}

func (r *serviceAccountResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy plan
	}
	if r.pd == nil {
		return
	}
	planCustomPropertiesAll(ctx, r.defaults, req, resp)
	planTagsAll(ctx, r.defaults, resp)
	planSPDefaults(ctx, r.defaults, r.pd.spDefs, kindCorpUser, resp)
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
			"custom_properties": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Arbitrary key-value metadata attached to the service account (the " +
					"`customProperties` field of the `corpUserInfo` aspect). Terraform owns the " +
					"complete map: keys added outside Terraform are removed on the next apply. Keys and " +
					"values must be non-empty strings, and values must not be null. Omit the attribute " +
					"entirely (do not set an empty map) to attach no custom properties. Provider-level " +
					"defaults (`auto_properties` markers and `defaults.custom_properties`) are merged in " +
					"automatically; the effective written map is the computed `custom_properties_all`.",
				Validators: []validator.Map{
					nonEmptyStringMapValidator{},
				},
			},
			"custom_properties_all":          customPropertiesAllSchema(),
			"tags_all":                       tagsAllSchema(),
			"structured_properties_defaults": spDefaultsSchema(),
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

	all, customProps, d := resolvePlannedCustomPropertiesAll(ctx, r.defaults, plan.CustomPropertiesAll, plan.CustomProperties, types.MapNull(types.StringType), true)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.CustomPropertiesAll = all

	urn, err := r.client.UpsertServiceAccount(ctx, plan.ServiceAccountID.ValueString(), strVal(plan.DisplayName), strVal(plan.Description), customProps)
	if err != nil {
		if errors.Is(err, datahub.ErrServiceAccountsUnsupported) {
			resp.Diagnostics.AddError("Service accounts not supported", err.Error())
		} else {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
		}
		return
	}

	tagsAll, tagURNs, td := resolvePlannedTagsAll(ctx, r.defaults, plan.TagsAll)
	resp.Diagnostics.Append(td...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.TagsAll = tagsAll
	if len(tagURNs) > 0 {
		if err := r.pd.ensureTagsExist(ctx, tagURNs); err != nil {
			resp.Diagnostics.AddError("Invalid provider defaults.tags", err.Error())
			return
		}
		if err := r.client.SetGlobalTags(ctx, corpUserEntityPath, urn, tagURNs); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	spAll, spVals, sd := resolvePlannedSPDefaults(r.defaults, r.pd.spDefs, kindCorpUser, plan.StructuredPropertiesDefaults)
	resp.Diagnostics.Append(sd...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.StructuredPropertiesDefaults = spAll
	if len(spVals) > 0 {
		resp.Diagnostics.Append(applySPDefaults(ctx, r.pd, urn, spVals, types.MapNull(spDefaultsElementType))...)
		if resp.Diagnostics.HasError() {
			return
		}
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
	tagsAll, err := readTagsAll(ctx, r.client, corpUserEntityPath, urn, state.TagsAll)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	state.TagsAll = tagsAll
	spDefaults, err := readSPDefaults(ctx, r.client, urn, state.StructuredPropertiesDefaults)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	state.StructuredPropertiesDefaults = spDefaults
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

	all, customProps, d := resolvePlannedCustomPropertiesAll(ctx, r.defaults, plan.CustomPropertiesAll, plan.CustomProperties, state.CustomPropertiesAll, false)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.CustomPropertiesAll = all

	if _, err := r.client.UpsertServiceAccount(ctx, plan.ServiceAccountID.ValueString(), strVal(plan.DisplayName), strVal(plan.Description), customProps); err != nil {
		if errors.Is(err, datahub.ErrServiceAccountsUnsupported) {
			resp.Diagnostics.AddError("Service accounts not supported", err.Error())
		} else {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
		}
		return
	}

	// Reconcile tags when the effective list changed. A null plan with a
	// non-null prior state clears the aspect and releases the ownership latch.
	if !plan.TagsAll.Equal(state.TagsAll) {
		tagsAll, tagURNs, td := resolvePlannedTagsAll(ctx, r.defaults, plan.TagsAll)
		resp.Diagnostics.Append(td...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.TagsAll = tagsAll
		if len(tagURNs) > 0 {
			if err := r.pd.ensureTagsExist(ctx, tagURNs); err != nil {
				resp.Diagnostics.AddError("Invalid provider defaults.tags", err.Error())
				return
			}
		}
		if err := r.client.SetGlobalTags(ctx, corpUserEntityPath, state.URN.ValueString(), tagURNs); err != nil {
			resp.Diagnostics.AddError("DataHub API Error", err.Error())
			return
		}
	}

	// Reconcile default structured properties when the managed set changed.
	if !plan.StructuredPropertiesDefaults.Equal(state.StructuredPropertiesDefaults) {
		spAll, spVals, sd := resolvePlannedSPDefaults(r.defaults, r.pd.spDefs, kindCorpUser, plan.StructuredPropertiesDefaults)
		resp.Diagnostics.Append(sd...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.StructuredPropertiesDefaults = spAll
		resp.Diagnostics.Append(applySPDefaults(ctx, r.pd, state.URN.ValueString(), spVals, state.StructuredPropertiesDefaults)...)
		if resp.Diagnostics.HasError() {
			return
		}
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
	// Import attribution: keys matching provider defaults or auto-property
	// markers are omitted from custom_properties so the first plan after
	// import is minimal.
	state.CustomProperties, state.CustomPropertiesAll = importCustomProperties(sa.CustomProperties, r.defaults)
	tagsAll, err := importTagsAll(ctx, r.client, r.defaults, corpUserEntityPath, sa.URN)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	state.TagsAll = tagsAll
	spDefaults, err := importSPDefaults(ctx, r.client, r.defaults, r.pd.spDefs, kindCorpUser, sa.URN)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	state.StructuredPropertiesDefaults = spDefaults
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// applyServiceAccountToModel maps a read service account (corpUser) onto the
// model, deriving the bare id from the URN and normalising optional fields to
// null when empty to avoid spurious drift. description reads from the raw
// corpUserInfo title (InfoTitle) so UI edits to editable title do not shadow it.
// Custom properties are reconciled against the model's prior state: the
// user-facing attribute keeps only its own keys, the computed _all records
// the full server map.
func applyServiceAccountToModel(sa *datahub.CorpUser, m *serviceAccountResourceModel) {
	m.URN = types.StringValue(sa.URN)
	m.ID = types.StringValue(sa.URN)
	m.ServiceAccountID = types.StringValue(datahub.ServiceAccountIDFromURN(sa.URN))
	m.DisplayName = nullIfEmpty(sa.DisplayName)
	m.Description = nullIfEmpty(sa.InfoTitle)
	m.CustomProperties, m.CustomPropertiesAll = reconcileCustomPropertiesRead(sa.CustomProperties, m.CustomProperties)
}
