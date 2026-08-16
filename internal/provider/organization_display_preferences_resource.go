// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ resource.Resource                = &organizationDisplayPreferencesResource{}
	_ resource.ResourceWithConfigure   = &organizationDisplayPreferencesResource{}
	_ resource.ResourceWithImportState = &organizationDisplayPreferencesResource{}
)

type organizationDisplayPreferencesResource struct {
	client *datahub.Client
}

type organizationDisplayPreferencesResourceModel struct {
	ID      types.String `tfsdk:"id"`
	URN     types.String `tfsdk:"urn"`
	OrgName types.String `tfsdk:"org_name"`
	LogoURL types.String `tfsdk:"logo_url"`
}

func NewOrganizationDisplayPreferencesResource() resource.Resource {
	return &organizationDisplayPreferencesResource{}
}

func (r *organizationDisplayPreferencesResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	r.client = pd.Client
}

func (r *organizationDisplayPreferencesResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_display_preferences"
}

func (r *organizationDisplayPreferencesResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: cloudOnlyBadge +
			"Manages the organization-wide display preferences shown in DataHub under " +
			"**Settings -> Preferences -> Appearance**, in the **Branding** section: the " +
			"organization name and logo that brand the UI for every user.\n\n" +
			"These are org-wide platform settings, not per-user preferences. The language " +
			"selector on the same settings page is a per-user choice and is deliberately not " +
			"managed by this provider.\n\n" +
			"## Singleton\n\n" +
			"DataHub stores these settings on a single, always-present global settings object, " +
			"so this resource is a singleton: there is no id to supply, and at most one instance " +
			"should exist in a configuration. Applying it updates the existing settings rather " +
			"than creating anything; `terraform destroy` resets the managed fields to DataHub's " +
			"defaults rather than deleting the settings object.\n\n" +
			"Because it is a singleton, a second instance in the same configuration (or the same " +
			"settings managed from two workspaces) will fight over the values on alternating " +
			"applies. Manage it from one place.\n\n" +
			"## Resetting a value\n\n" +
			"Omitting an attribute, setting it to an empty string, or destroying the resource all " +
			"reset that field to DataHub's default branding. DataHub has no way to remove the " +
			"underlying value once written, so the field is stored as empty rather than removed - " +
			"the effect in the UI is the same.\n\n" +
			"Organization display preferences are a DataHub Cloud capability. DataHub Cloud upgrades " +
			"on its own release cadence, so a release may occasionally affect this resource; fixes " +
			"are handled in the provider. Pin the provider version for client-side stability and " +
			"upgrade it to pick up fixes (including any needed for backend changes), and please " +
			"open an issue if you hit one.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Always the global settings URN (`" + datahub.GlobalSettingsURN + "`). This resource is a singleton and takes no user-supplied id.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "URN of the DataHub global settings singleton (`" + datahub.GlobalSettingsURN + "`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"org_name": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Organization name used to brand the DataHub UI (browser title and " +
					"navigation). Omit or set to an empty string to fall back to DataHub's default title.",
			},
			"logo_url": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "URL of the organization logo shown in the DataHub UI. Must be reachable " +
					"by the browsers of users viewing DataHub. Omit or set to an empty string to fall back to " +
					"the default DataHub logo.",
			},
		},
	}
}

// apply writes the desired display preferences and returns the state to
// persist. It is shared by Create and Update: for a singleton there is no
// distinction, both simply move the server to the configured values.
func (r *organizationDisplayPreferencesResource) apply(
	ctx context.Context,
	plan organizationDisplayPreferencesResourceModel,
) (organizationDisplayPreferencesResourceModel, error) {
	want := datahub.OrganizationDisplayPreferences{
		OrgName: plan.OrgName.ValueString(),
		LogoURL: plan.LogoURL.ValueString(),
	}
	if err := r.client.SetOrganizationDisplayPreferences(ctx, want); err != nil {
		return plan, fmt.Errorf("writing organization display preferences: %w", err)
	}

	state := organizationDisplayPreferencesResourceModel{
		ID:  types.StringValue(datahub.GlobalSettingsURN),
		URN: types.StringValue(datahub.GlobalSettingsURN),
		// Preserve the configured nullness rather than echoing the server's
		// empty strings, so an omitted attribute stays null in state and the
		// next plan is clean.
		OrgName: plan.OrgName,
		LogoURL: plan.LogoURL,
	}
	return state, nil
}

func (r *organizationDisplayPreferencesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan organizationDisplayPreferencesResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state, err := r.apply(ctx, plan)
	if err != nil {
		r.addWriteError(resp.Diagnostics.AddError, err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *organizationDisplayPreferencesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state organizationDisplayPreferencesResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	prefs, found, err := r.client.GetOrganizationDisplayPreferences(ctx)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if !found {
		// The singleton is absent, which means there is nothing to manage on
		// this instance. Drop it from state so the next plan recreates it.
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(datahub.GlobalSettingsURN)
	state.URN = types.StringValue(datahub.GlobalSettingsURN)
	state.OrgName = canonicalDisplayPreference(state.OrgName, prefs.OrgName)
	state.LogoURL = canonicalDisplayPreference(state.LogoURL, prefs.LogoURL)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *organizationDisplayPreferencesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan organizationDisplayPreferencesResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state, err := r.apply(ctx, plan)
	if err != nil {
		r.addWriteError(resp.Diagnostics.AddError, err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Delete resets the managed fields to DataHub's defaults. The global settings
// singleton itself is never deleted: it is platform-level state that DataHub
// always expects to exist, and other sections of it (SSO, notifications,
// integrations, the default home-page template) are not this resource's to
// remove.
func (r *organizationDisplayPreferencesResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	if err := r.client.SetOrganizationDisplayPreferences(ctx, datahub.OrganizationDisplayPreferences{}); err != nil {
		r.addWriteError(resp.Diagnostics.AddError, err)
		return
	}
	resp.State.RemoveResource(ctx)
}

// ImportState brings the existing settings under management. The URN is fixed,
// so any import id is accepted (including the URN itself or a placeholder such
// as "-"); the value is not used.
func (r *organizationDisplayPreferencesResource) ImportState(ctx context.Context, _ resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	prefs, found, err := r.client.GetOrganizationDisplayPreferences(ctx)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError(
			"Global settings not found",
			"The DataHub global settings object was not found on this instance, so organization "+
				"display preferences cannot be imported.",
		)
		return
	}

	state := organizationDisplayPreferencesResourceModel{
		ID:      types.StringValue(datahub.GlobalSettingsURN),
		URN:     types.StringValue(datahub.GlobalSettingsURN),
		OrgName: optionalStringValue(prefs.OrgName),
		LogoURL: optionalStringValue(prefs.LogoURL),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// addWriteError maps a client error onto a diagnostic, giving the Cloud-only
// case a clear message instead of a raw GraphQL schema error.
func (r *organizationDisplayPreferencesResource) addWriteError(add func(string, string), err error) {
	if errors.Is(err, datahub.ErrOrganizationDisplayPreferencesCloudOnly) {
		add(
			"Organization display preferences require DataHub Cloud",
			"This DataHub instance does not support organization display preferences. "+
				"The setting is available on DataHub Cloud only; remove the "+
				"datahub_organization_display_preferences resource when targeting open-source DataHub.",
		)
		return
	}
	add("DataHub API Error", err.Error())
}

// canonicalDisplayPreference reconciles a server value against the prior
// configured value. DataHub cannot remove these fields, only blank them, so an
// empty server value is equivalent to "not set": keep it null in state when the
// configuration left it null, and only surface a real string when the server
// has one. Without this, an omitted attribute would drift null -> "" forever.
func canonicalDisplayPreference(prior types.String, server string) types.String {
	if server == "" {
		if prior.IsNull() {
			return prior
		}
		// The configuration asked for a value (possibly "") and the server is
		// blank: report the server's empty string so real drift is visible.
		return types.StringValue("")
	}
	return types.StringValue(server)
}

// optionalStringValue maps a server value to a null Terraform string when
// blank, so an imported unset field does not show a spurious "" -> null diff.
func optionalStringValue(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
