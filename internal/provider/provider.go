// Copyright 2026 The DataHub Project Authors
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"

	// Side-effect import: registers every import target (enumeration + URN->ID
	// mapping) with the importtarget registry that the enumeration data sources
	// and the importtarget coverage test rely on. The same package is imported by
	// the datahub-tf-extract CLI, so both share one source of truth.
	_ "github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget/targets"
)

// removedCLIConfigFallbackHint is appended to both missing-credential
// diagnostics. Earlier versions fell back to reading gms.server and gms.token
// from the DataHub CLI's ~/.datahubenv whenever the configuration and the
// environment were both empty, so a config that supplied neither still
// authenticated - against whichever instance the operator's CLI had last been
// pointed at. A user upgrading hits this message at precisely the moment the
// removal affects them, which is the only moment the explanation is useful.
//
// Deliberately does not name the release the removal landed in. The version
// would be a hardcoded literal that silently becomes a lie if the change ships
// in a different release than planned, and nothing in a diagnostic can verify
// it. The CHANGELOG carries the version, is maintained as part of release prep,
// and is where a reader goes for "which version" anyway.
const removedCLIConfigFallbackHint = "\n\n" +
	"If this configuration previously worked without either, the provider was " +
	"authenticating from ~/.datahubenv (your DataHub CLI configuration). That " +
	"fallback has been removed: it made the target instance depend on the " +
	"machine rather than on the configuration, so the same Terraform could apply " +
	"to different environments - including production - with nothing in the " +
	"configuration to say so. See the CHANGELOG for the release this changed in. " +
	"Set the values explicitly instead; to keep using the CLI's values, export " +
	"them: " +
	`DATAHUB_GMS_URL=$(yq '.gms.server' ~/.datahubenv) ` +
	`DATAHUB_GMS_TOKEN=$(yq '.gms.token' ~/.datahubenv)`

// Ensure datahubProvider satisfies various provider interfaces.
var _ provider.Provider = &datahubProvider{}

// datahubProvider defines the provider implementation.
type datahubProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// New is a helper function to simplify provider server and testing implementation.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &datahubProvider{
			version: version,
		}
	}
}

// datahubProviderModel describes the provider data model.
type datahubProviderModel struct {
	GmsURL               types.String `tfsdk:"gms_url"`
	GmsToken             types.String `tfsdk:"gms_token"`
	FrontendURL          types.String `tfsdk:"frontend_url"`
	Defaults             types.Object `tfsdk:"defaults"`
	AutoProperties       types.Set    `tfsdk:"auto_properties"`
	AutoPropertyStrategy types.String `tfsdk:"auto_property_strategy"`
}

// Metadata returns the provider type name.
func (p *datahubProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "datahub"
	resp.Version = p.version
}

// Schema defines the provider-level schema for configuration data.
func (p *datahubProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Terraform provider for managing DataHub platform configuration as code.\n\n" +
			"**What this provider manages:** Platform-level configuration that controls how metadata " +
			"flows into DataHub -- ingestion source recipes, encrypted secrets referenced in those " +
			"recipes, and Remote Executor Pool registrations for private-network ingestion.\n\n" +
			"**What this provider does not do:** It does not provision a DataHub instance; for that, " +
			"see [DataHub Cloud](https://datahub.com/cloud) or the " +
			"[DataHub deployment guides](https://docs.datahub.com/docs/category/deployment-guides). " +
			"It also does not manage the data assets and metadata that DataHub ingests -- datasets, " +
			"dashboards, tags, glossary terms, ownership, and similar enrichment are populated by " +
			"your ingestion pipelines, not Terraform.\n\n" +
			"**Terraform version:** Most resources work with any recent Terraform version. " +
			"Resources that use WriteOnly attributes (`datahub_secret`, `datahub_connection`) " +
			"require Terraform >= 1.11; add `required_version = \">= 1.11\"` to your " +
			"`terraform {}` block when using those resources.",
		Attributes: map[string]schema.Attribute{
			"gms_url": schema.StringAttribute{
				MarkdownDescription: "DataHub GMS URL. For example: `https://datahub.example.com`. " +
					"If not set, the provider reads `DATAHUB_GMS_URL` from the environment. " +
					"There is no further fallback: the provider does not read the DataHub CLI's " +
					"`~/.datahubenv`, so the target instance is determined by this configuration " +
					"or the environment rather than by the machine it runs on. " +
					"A plaintext `http://` endpoint to a non-loopback host raises a warning, " +
					"because the token travels as a bearer credential in cleartext.",
				Optional: true,
			},
			"gms_token": schema.StringAttribute{
				MarkdownDescription: "DataHub GMS token for authentication. " +
					"If not set, the provider reads `DATAHUB_GMS_TOKEN` from the environment. " +
					"There is no further fallback -- see `gms_url`. Prefer supplying a short-lived " +
					"token from a secrets manager or credential broker over a long-lived personal " +
					"access token held in a variables file.",
				Optional:  true,
				Sensitive: true,
			},
			"frontend_url": schema.StringAttribute{
				MarkdownDescription: "DataHub frontend URL for native user operations (sign-up, password reset). " +
					"For example: `https://datahub.example.com:9002`. " +
					"If not set, the provider reads `DATAHUB_FRONTEND_URL` from the environment, " +
					"or derives it from `gms_url` by stripping any `/gms` suffix and replacing " +
					"port 8080 with 9002. Only needed when using `datahub_local_user_login`.",
				Optional: true,
			},
			"defaults": schema.SingleNestedAttribute{
				Optional: true,
				MarkdownDescription: "Default labels attached to every resource this provider manages, " +
					"wherever the underlying DataHub entity type supports them (similar in spirit to the " +
					"AWS provider's `default_tags`). Resource-level values win over provider defaults on a " +
					"per-key basis; a differing value raises a plan-time warning. Resources whose entity " +
					"type supports no default mechanism (for example `datahub_ingestion_source`, " +
					"`datahub_secret`, `datahub_policy`) are unaffected.",
				Attributes: map[string]schema.Attribute{
					"custom_properties": schema.MapAttribute{
						Optional:    true,
						ElementType: types.StringType,
						MarkdownDescription: "Custom properties merged into the `custom_properties` of " +
							"every resource whose entity type supports them: `datahub_domain`, " +
							"`datahub_glossary_term`, `datahub_glossary_node`, `datahub_corp_user`, " +
							"`datahub_service_account`, and `datahub_data_product`. Resource-level keys " +
							"win; the provider owns the complete server-side map on managed entities, so " +
							"properties added outside Terraform are removed on the next apply. The " +
							"effective merged map is exposed on each resource as the computed " +
							"`custom_properties_all` attribute.",
						Validators: []validator.Map{nonEmptyStringMapValidator{}},
					},
					"tags": schema.SetAttribute{
						Optional:    true,
						ElementType: types.StringType,
						MarkdownDescription: "Tag URNs (`urn:li:tag:...`) attached to every resource whose " +
							"entity type supports the `globalTags` aspect: `datahub_corp_user`, " +
							"`datahub_service_account`, `datahub_corp_group`, `datahub_data_product`, " +
							"and the assertion resources (`datahub_custom_assertion`, " +
							"`datahub_field_assertion`, `datahub_freshness_assertion`, " +
							"`datahub_schema_assertion`, `datahub_sql_assertion`, " +
							"`datahub_volume_assertion`). " +
							"While set, the provider owns the complete tag list on managed entities (tags " +
							"added outside Terraform are removed on the next apply); the effective list is " +
							"exposed on each resource as the computed `tags_all` attribute. Referenced tags " +
							"must already exist - create them in a separate apply. When this attribute has " +
							"never been set for a resource, the provider neither reads nor writes its tags.",
						Validators: []validator.Set{urnPrefixSetValidator{prefix: tagURNPrefix}},
					},
					"structured_properties": schema.MapAttribute{
						Optional:    true,
						ElementType: types.SetType{ElemType: types.StringType},
						MarkdownDescription: "Structured property values applied to every resource whose " +
							"entity type supports them (`datahub_domain`, `datahub_glossary_term`, " +
							"`datahub_glossary_node`, `datahub_corp_user`, `datahub_service_account`, " +
							"`datahub_corp_group`, `datahub_data_product`, `datahub_data_contract`), keyed " +
							"by property URN (`urn:li:structuredProperty:...`). Ownership is per property: " +
							"only the properties named here are managed, so properties assigned via " +
							"`datahub_structured_property_assignment` or outside Terraform are untouched - " +
							"but a property URN listed here should not also be managed by an assignment " +
							"resource on the same entity. Definitions must already exist (create them in a " +
							"separate apply) and are applied only to resources whose entity type appears in " +
							"the definition's `entity_types`; other resources skip the property. The " +
							"effective managed subset is exposed on each resource as the computed " +
							"`structured_properties_defaults` attribute.",
						Validators: []validator.Map{spDefaultsMapValidator{}},
					},
				},
			},
			"auto_properties": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Provenance markers automatically added to the custom properties of " +
					"every managed resource whose entity type supports custom properties (same resource " +
					"list as `defaults.custom_properties`). Allowed markers: `managed-by` (writes " +
					"`managed-by = \"terraform\"`) and `provider-version` (writes " +
					"`provider-version = \"<provider version>\"`). Defaults to `[\"managed-by\"]`; set to " +
					"`[]` to disable. Removing a marker from this list removes the property from all " +
					"managed entities on the next apply, regardless of `auto_property_strategy`. " +
					"Explicitly configured keys of the same name (in `defaults.custom_properties` or a " +
					"resource's `custom_properties`) always take precedence over markers.",
				Validators: []validator.Set{enumSet(autoPropertyManagedBy, autoPropertyProviderVersion)},
			},
			"auto_property_strategy": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "When auto properties are stamped. `CREATION_ONLY` (default): markers " +
					"are added only when an entity is created and their values are frozen at creation, so " +
					"upgrading the provider never produces diffs on existing resources. `PROACTIVE`: " +
					"markers and their current values are enforced on every managed entity on every apply. " +
					"Use `PROACTIVE` once to converge an estate created before this feature (or with " +
					"earlier provider versions), or leave it on to keep `provider-version` current at the " +
					"cost of diffs after every provider upgrade.",
				Validators: []validator.String{enumString(autoPropertyStrategyCreationOnly, autoPropertyStrategyProactive)},
			},
		},
	}
}

// Configure prepares a HashiCups API client for data sources and resources.
func (p *datahubProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config datahubProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// If practitioner provided a configuration value for any of the
	// attributes, it must be a known value.
	if config.GmsURL.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("gms_url"),
			"Unknown DataHub GMS URL (DATAHUB_GMS_URL)",
			"The provider cannot create the Datahub API client as there is an unknown configuration value for the DataHub GMS URL. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the DATAHUB_GMS_URL environment variable.",
		)
	}

	if config.GmsToken.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("gms_token"),
			"Unknown DataHub GMS Token (DATAHUB_GMS_TOKEN)",
			"The provider cannot create the Datahub API client as there is an unknown configuration value for the Datahub GMS token. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the DATAHUB_GMS_TOKEN environment variable.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Default values to environment variables, but override
	// with Terraform configuration value if set.
	host := os.Getenv("DATAHUB_GMS_URL")
	gmsToken := os.Getenv("DATAHUB_GMS_TOKEN")

	if !config.GmsURL.IsNull() {
		host = config.GmsURL.ValueString()
	}

	if !config.GmsToken.IsNull() {
		gmsToken = config.GmsToken.ValueString()
	}

	// Trim before the emptiness checks below, so a whitespace-only value fails
	// with the actionable diagnostic rather than being handed to the HTTP client
	// as a malformed endpoint or a bearer token of spaces.
	host = strings.TrimSpace(host)
	gmsToken = strings.TrimSpace(gmsToken)

	// There is deliberately no third fallback. Earlier versions read
	// gms.server / gms.token from the DataHub CLI's ~/.datahubenv when both the
	// configuration and the environment were empty. See
	// removedCLIConfigFallbackHint and docs/design/provider-credential-resolution.md.
	if host == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("gms_url"),
			"Missing DataHub GMS URL (DATAHUB_GMS_URL)",
			"The provider cannot create the DataHub API client because no GMS URL was supplied. "+
				"Set gms_url in the provider configuration, or the DATAHUB_GMS_URL environment variable."+
				removedCLIConfigFallbackHint,
		)
	}
	if gmsToken == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("gms_token"),
			"Missing DataHub GMS Token (DATAHUB_GMS_TOKEN)",
			"The provider cannot create the DataHub API client because no GMS token was supplied. "+
				"Set gms_token in the provider configuration, or the DATAHUB_GMS_TOKEN environment variable."+
				removedCLIConfigFallbackHint,
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	warnIfPlaintextEndpoint(resp, host)

	// Create a new Datahub client using the configuration values
	client, err := datahub.NewClient(host, gmsToken)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Datahub API Client",
			"An unexpected error occurred when creating the Datahub API client. "+
				"If the error is not clear, please contact the provider developers.\n\n"+
				"Datahub Client Error: "+err.Error(),
		)
		return
	}

	// Resolve frontend URL: config > env var > heuristic from GMS URL.
	frontendURL := os.Getenv("DATAHUB_FRONTEND_URL")
	if !config.FrontendURL.IsNull() {
		frontendURL = config.FrontendURL.ValueString()
	}
	if frontendURL != "" {
		client.SetFrontendURL(frontendURL)
	}

	identity, err := client.Me(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to authenticate with DataHub",
			fmt.Sprintf("The configured gms_url/gms_token could not be verified against %s: %s", client.BaseURL(), err),
		)
		return
	}
	tflog.Info(ctx, "Authenticated with DataHub", map[string]any{
		"urn":     identity.Urn,
		"version": p.version,
	})

	defaults, defaultsDiags := parseEntityDefaults(ctx, config.Defaults, config.AutoProperties, config.AutoPropertyStrategy, p.version)
	resp.Diagnostics.Append(defaultsDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Prefetch defaults.structured_properties definitions once for plan-time
	// entityTypes filtering. Failures warn rather than error: a hard error
	// here would block `terraform destroy` of configs whose definitions are
	// already deleted. Skipped-at-plan properties hard-error at apply time if
	// a write is attempted (providerData.ensureSPDef).
	spDefs := map[string]*datahub.StructuredProperty{}
	if !defaults.StructuredProperties.IsNull() && !defaults.StructuredProperties.IsUnknown() {
		for urn := range defaults.StructuredProperties.Elements() {
			def, err := client.GetStructuredPropertyByURN(ctx, urn)
			if err != nil {
				resp.Diagnostics.AddWarning(
					"Could not fetch default structured property definition",
					fmt.Sprintf("Fetching %s failed (%s); resources will skip this default until it can be resolved.", urn, err),
				)
			} else if def == nil {
				resp.Diagnostics.AddWarning(
					"Default structured property does not exist",
					fmt.Sprintf("%s (referenced in defaults.structured_properties) was not found; resources will skip this default. Create the definition in a separate apply before relying on it.", urn),
				)
			}
			spDefs[urn] = def
		}
	}

	// Data sources receive the bare client; resources additionally receive
	// the provider-level defaults configuration.
	resp.DataSourceData = client
	resp.ResourceData = &providerData{
		Client:   client,
		defaults: defaults,
		spDefs:   spDefs,
	}
}

// DataSources defines the data sources implemented in the provider.
func (p *datahubProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewActionPipelinesDataSource,
		NewAssertionDataSource,
		NewAssertionsDataSource,
		NewAssertionAssignmentRulesDataSource,
		NewConnectionsDataSource,
		NewCorpGroupDataSource,
		NewCorpGroupsDataSource,
		NewCorpUserDataSource,
		NewDataContractsDataSource,
		NewDataProductDataSource,
		NewDataProductsDataSource,
		NewDomainDataSource,
		NewDomainsDataSource,
		NewGlossaryNodeDataSource,
		NewGlossaryNodesDataSource,
		NewGlossaryTermDataSource,
		NewGlossaryTermsDataSource,
		NewIngestionSourceDataSource,
		NewIngestionSourcesDataSource,
		NewMeDataSource,
		NewOrganizationDisplayPreferencesDataSource,
		NewOwnershipTypeDataSource,
		NewOwnershipTypesDataSource,
		NewPoliciesDataSource,
		NewRemoteExecutorPoolDataSource,
		NewRoleDataSource,
		NewRolesDataSource,
		NewSecretsDataSource,
		NewServiceAccountDataSource,
		NewServiceAccountsDataSource,
		NewStructuredPropertiesDataSource,
		NewStructuredPropertyDataSource,
		NewTagDataSource,
		NewTagsDataSource,
	}
}

// Resources defines the resources implemented in the provider.
func (p *datahubProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewActionPipelineResource,
		NewAssertionAssignmentRuleResource,
		NewCustomAssertionResource,
		NewConnectionResource,
		NewCorpGroupResource,
		NewCorpGroupMemberResource,
		NewCorpUserResource,
		NewDataContractResource,
		NewDataProductResource,
		NewDomainResource,
		NewGlossaryNodeResource,
		NewGlossaryTermResource,
		NewFieldAssertionResource,
		NewFreshnessAssertionResource,
		NewLocalUserLoginResource,
		NewOrganizationDisplayPreferencesResource,
		NewOwnershipTypeResource,
		NewPageModuleResource,
		NewIngestionSourceResource,
		NewPolicyResource,
		NewSecretResource,
		NewServiceAccountResource,
		NewRemoteExecutorPoolResource,
		NewRoleAssignmentResource,
		NewSchemaAssertionResource,
		NewSQLAssertionResource,
		NewStructuredPropertyResource,
		NewStructuredPropertyAssignmentResource,
		NewTagResource,
		NewVolumeAssertionResource,
	}
}
