// Copyright 2026 The DataHub Project Authors
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"gopkg.in/yaml.v3"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"

	// Side-effect import: registers every import target (enumeration + URN->ID
	// mapping) with the importtarget registry that the enumeration data sources
	// and the importtarget coverage test rely on. The same package is imported by
	// the datahub-tf-extract CLI, so both share one source of truth.
	_ "github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget/targets"
)

type datahubEnvConfig struct {
	Gms struct {
		Server string `yaml:"server"`
		Token  string `yaml:"token"`
	} `yaml:"gms"`
}

func readDatahubEnvConfig() (datahubEnvConfig, bool, error) {
	var cfg datahubEnvConfig

	home, err := os.UserHomeDir()
	if err != nil {
		return cfg, false, fmt.Errorf("determining home directory: %w", err)
	}

	path := filepath.Join(home, ".datahubenv")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return cfg, false, nil
		}
		return cfg, false, fmt.Errorf("checking %s: %w", path, err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return cfg, false, fmt.Errorf("reading %s: %w", path, err)
	}

	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return cfg, false, fmt.Errorf("parsing %s: %w", path, err)
	}

	return cfg, true, nil
}

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
	GmsURL      types.String `tfsdk:"gms_url"`
	GmsToken    types.String `tfsdk:"gms_token"`
	FrontendURL types.String `tfsdk:"frontend_url"`
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
					"If not set, the provider will read `DATAHUB_GMS_URL` from the environment, " +
					"or fall back to `gms.server` in `~/.datahubenv`.",
				Optional: true,
			},
			"gms_token": schema.StringAttribute{
				MarkdownDescription: "DataHub GMS token for authentication. " +
					"If not set, the provider will read the token from the `DATAHUB_GMS_TOKEN` environment variable, " +
					"or fall back to the local DataHub CLI configuration at `~/.datahubenv`.",
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

	// Last resort: Datahub CLI local configuration at ~/.datahubenv
	if host == "" || gmsToken == "" {
		envCfg, exists, err := readDatahubEnvConfig()
		if err != nil {
			resp.Diagnostics.AddError(
				"Unable to Read Datahub CLI Configuration",
				"The provider attempted to read ~/.datahubenv but encountered an error. "+err.Error(),
			)
			return
		}
		if exists {
			if host == "" && envCfg.Gms.Server != "" {
				host = strings.TrimSpace(envCfg.Gms.Server)
			}
			if gmsToken == "" && envCfg.Gms.Token != "" {
				gmsToken = envCfg.Gms.Token
			}
		}
	}

	if host == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("gms_url"),
			"Missing DataHub GMS URL (DATAHUB_GMS_URL)",
			"The provider cannot create the Datahub API client as there is a missing or empty value for the DataHub GMS URL. "+
				"Set gms_url in the configuration or use the DATAHUB_GMS_URL environment variable. "+
				"If unconfigured, run `datahub init` to create ~/.datahubenv.",
		)
	}
	if gmsToken == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("gms_token"),
			"Missing DataHub GMS Token (DATAHUB_GMS_TOKEN)",
			"The provider cannot create the Datahub API client as there is a missing or empty value for the Datahub GMS token. "+
				"Set the gms_token value in the configuration or use the DATAHUB_GMS_TOKEN environment variable. "+
				"If unconfigured, run `datahub init` to create ~/.datahubenv.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

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

	// Data sources receive the bare client; resources additionally receive
	// the provider-level defaults configuration. The provider schema for
	// defaults is introduced together with the first consuming resources, so
	// the defaults are currently always empty.
	resp.DataSourceData = client
	resp.ResourceData = &providerData{
		Client:   client,
		defaults: emptyEntityDefaults(p.version),
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
		NewOwnershipTypeResource,
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
