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
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// hexColorOrEmptyValidator applies the provider's existing #RRGGBB rule
// (hexColorValidator, shared with datahub_tag.color_hex) while exempting the
// empty string, which is DataHub's documented sentinel for "clear this and
// restore the default brand" rather than a malformed colour.
//
// Validating the shape at plan time rather than letting the server decide is a
// departure from the usual preference for server-side rejection, and it is
// chosen on failure mode (docs/roadmap.md, "Version and capability
// compatibility"): a server that rejects cleanly wants its error translated,
// but whether DataHub rejects a malformed brand colour at all has not been
// observed. If it stores "red" verbatim, the UI derives its brand tokens from
// nonsense and nothing reports it. One regexp covers that case; nothing else
// does.
type hexColorOrEmptyValidator struct{}

func (v hexColorOrEmptyValidator) Description(ctx context.Context) string {
	return hexColorValidator{}.Description(ctx) + `, or "" to restore DataHub's default brand`
}

func (v hexColorOrEmptyValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v hexColorOrEmptyValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() || req.ConfigValue.ValueString() == "" {
		return
	}
	hexColorValidator{}.ValidateString(ctx, req, resp)
}

var (
	_ resource.Resource                = &organizationDisplayPreferencesResource{}
	_ resource.ResourceWithConfigure   = &organizationDisplayPreferencesResource{}
	_ resource.ResourceWithImportState = &organizationDisplayPreferencesResource{}
)

type organizationDisplayPreferencesResource struct {
	client *datahub.Client
}

type organizationDisplayPreferencesResourceModel struct {
	ID           types.String `tfsdk:"id"`
	URN          types.String `tfsdk:"urn"`
	OrgName      types.String `tfsdk:"org_name"`
	LogoURL      types.String `tfsdk:"logo_url"`
	PrimaryColor types.String `tfsdk:"primary_color"`
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
			"organization name, logo and brand colour that brand the UI for every user.\n\n" +
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
			"Setting an attribute to an empty string, removing an attribute you had previously " +
			"set, or destroying the resource all reset that field to DataHub's default branding. " +
			"DataHub has no way to remove the underlying value once written, so the field is " +
			"stored as empty rather than removed - the effect in the UI is the same.\n\n" +
			"`primary_color` differs in one case, and only one: while you have **never** set it, " +
			"this resource does not touch it, so a colour set in the DataHub UI survives an apply " +
			"that does not mention it. Set it once - to `\"\"` if what you want is DataHub's " +
			"default - and it is owned from then on like every other attribute here, including " +
			"being reset if you later remove the line. The exception exists because the brand " +
			"colour is newer than this resource: blanking it for everyone who has not asked for " +
			"it would break configurations that work today against DataHub Cloud instances that " +
			"predate the field.\n\n" +
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
			"primary_color": schema.StringAttribute{
				Optional:   true,
				Validators: []validator.String{hexColorOrEmptyValidator{}},
				MarkdownDescription: newInProviderNote("v0.25.0") +
					"Brand colour for the DataHub UI, as a hex colour such as `#EC0016`. " +
					"DataHub derives the UI's brand tokens from it, so it affects more than one element. " +
					"Set it to an empty string to restore DataHub's default brand.\n\n" +
					"Leaving it out is safe on any version: the " +
					"provider sends the field only once you have set it, so a configuration that never " +
					"mentions it works unchanged against an older instance. Unlike the other attributes " +
					"here, leaving it out is also not a reset while you have never set it - see " +
					"*Resetting a value* above.",
			},
		},
	}
}

// apply writes the desired display preferences and returns the state to
// persist. It is shared by Create and Update: for a singleton there is no
// distinction, both simply move the server to the configured values.
//
// priorPrimaryColor is the value state held before this apply - null on Create.
// It is what lets a removed primary_color still be cleared; see
// primaryColorToWrite.
func (r *organizationDisplayPreferencesResource) apply(
	ctx context.Context,
	plan organizationDisplayPreferencesResourceModel,
	priorPrimaryColor types.String,
) (organizationDisplayPreferencesResourceModel, error) {
	want := datahub.OrganizationDisplayPreferencesUpdate{
		OrgName:      plan.OrgName.ValueString(),
		LogoURL:      plan.LogoURL.ValueString(),
		PrimaryColor: primaryColorToWrite(plan.PrimaryColor, priorPrimaryColor),
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
		OrgName:      plan.OrgName,
		LogoURL:      plan.LogoURL,
		PrimaryColor: plan.PrimaryColor,
	}
	return state, nil
}

// primaryColorToWrite decides whether this write carries primaryColor at all,
// and with what value. It returns nil to leave the field out of the mutation
// entirely.
//
// Three cases, and the middle one is the whole point:
//
//   - Configured (including ""): send it. "" is DataHub's clear-to-default
//     sentinel, so an explicit empty string is a real instruction, not an
//     absence.
//   - Not configured now, but present in prior state: the practitioner has
//     removed a line they previously had. Send "" so removal resets the field,
//     matching org_name and logo_url. Without this the apply would report
//     success, write null into state, and leave the server holding a colour
//     nothing now claims - state lying about the instance, permanently and
//     silently, because Read below will not re-adopt it either.
//   - Never configured: send nothing. This is the compatibility guarantee.
//     primaryColor does not exist in the mutation input before DataHub Cloud
//     v2.2.0, and GraphQL rejects a variable carrying an undefined input field
//     before executing, so a user on an older Cloud who never asks for a brand
//     colour must generate a mutation that never names one. It also means a
//     colour set in the DataHub UI is left alone until Terraform is told to
//     own it.
func primaryColorToWrite(planned, prior types.String) *string {
	if !planned.IsNull() && !planned.IsUnknown() {
		v := planned.ValueString()
		return &v
	}
	if !prior.IsNull() && !prior.IsUnknown() {
		cleared := ""
		return &cleared
	}
	return nil
}

func (r *organizationDisplayPreferencesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan organizationDisplayPreferencesResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Create has no prior state, so a primary_color absent from the config has
	// never been managed here and must not be written.
	state, err := r.apply(ctx, plan, types.StringNull())
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
	state.PrimaryColor = canonicalPrimaryColor(state.PrimaryColor, prefs.PrimaryColor)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *organizationDisplayPreferencesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior organizationDisplayPreferencesResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state, err := r.apply(ctx, plan, prior.PrimaryColor)
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
func (r *organizationDisplayPreferencesResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state organizationDisplayPreferencesResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The brand colour is reset only if this resource was managing it. A
	// destroy of a configuration that never set primary_color must not name the
	// field, both because there is nothing of ours to undo and because an
	// older DataHub Cloud would reject the whole mutation over it - which would
	// make the resource impossible to destroy.
	want := datahub.OrganizationDisplayPreferencesUpdate{
		PrimaryColor: primaryColorToWrite(types.StringNull(), state.PrimaryColor),
	}
	if err := r.client.SetOrganizationDisplayPreferences(ctx, want); err != nil {
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
		// Import adopts whatever the instance holds, as it does for the other
		// two fields, so an imported colour is owned from that moment and a
		// config omitting it will reset it. On an instance predating the field
		// the read yields "" and this stays null, so nothing is adopted and
		// nothing is later sent.
		PrimaryColor: optionalStringValue(prefs.PrimaryColor),
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
	var unsupported *datahub.UnsupportedFieldError
	if errors.As(err, &unsupported) {
		add(
			"DataHub does not support "+unsupported.Attribute,
			unsupported.Error()+"\n\nLeaving "+unsupported.Attribute+" unset manages the "+
				"remaining attributes as before.",
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

// canonicalPrimaryColor reconciles the server's brand colour against the prior
// configured value, and differs from canonicalDisplayPreference in exactly one
// case: a null prior stays null whatever the server holds.
//
// That case is the difference between "this resource owns the field" and "this
// resource has not been told to". Adopting an undeclared server value would
// make it look like drift, and the next apply would clear a colour the
// practitioner set in the DataHub UI and never asked Terraform to touch. Once
// the attribute is set - even to "" - the prior is non-null and the server
// value is surfaced, so real drift against a managed colour is still caught.
func canonicalPrimaryColor(prior types.String, server string) types.String {
	if prior.IsNull() {
		return prior
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
