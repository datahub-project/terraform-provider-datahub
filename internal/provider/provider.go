// Copyright 2026 The DataHub Project Authors
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"gopkg.in/yaml.v3"
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
		return cfg, false, err
	}

	path := filepath.Join(home, ".datahubenv")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return cfg, false, nil
		}
		return cfg, false, err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return cfg, false, err
	}

	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return cfg, false, err
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
	Host     types.String `tfsdk:"host"`
	GmsToken types.String `tfsdk:"gms_token"`
}

// Metadata returns the provider type name.
func (p *datahubProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "datahub"
	resp.Version = p.version
}

// Schema defines the provider-level schema for configuration data.
func (p *datahubProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Terraform provider for managing DataHub ingestion sources via the DataHub APIs.\n\n" +
			"**Security note:** DataHub ingestion recipes and source configurations are stored in DataHub. If you embed credentials (tokens, passwords, private keys) directly into a recipe/config, they can end up stored in DataHub metadata and exposed to users/services with access to view ingestion source configs.\n\n" +
			"For production, prefer DataHub Secrets and environment variable substitution (e.g. `${SECRET_NAME}` / `${MY_PASSWORD}`) instead of hard-coding credentials. See https://docs.datahub.com/docs/ui-ingestion/#configuring-secrets and https://docs.datahub.com/docs/metadata-ingestion/recipe_overview#handling-sensitive-information-in-recipes.",
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				MarkdownDescription: "Datahub instance host URL. For eg. https://datahub.example.com. " +
					"If not filled the provider will attempt to read the host from the DATAHUB_HOST environment variable and " +
					"as last resort from the local Datahub CLI configuration located at ~/.datahubenv",
				Optional: true,
			},
			"gms_token": schema.StringAttribute{
				MarkdownDescription: "Datahub GMS token for authentication." +
					"If not filled, the provider will attempt to read the token from the DATAHUB_GMS_TOKEN environment variable and " +
					"as a last resort, from the local Datahub CLI configuration located at ~/.datahubenv.",
				Optional:  true,
				Sensitive: true,
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
	if config.Host.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Unknown Datahub API Host",
			"The provider cannot create the Datahub API client as there is an unknown configuration value for the Datahub API host. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the DATAHUB_HOST environment variable.",
		)
	}

	if config.GmsToken.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("gms_token"),
			"Unknown Datahub GMS Token",
			"The provider cannot create the Datahub API client as there is an unknown configuration value for the Datahub GMS token. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the DATAHUB_GMS_TOKEN environment variable.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Default values to environment variables, but override
	// with Terraform configuration value if set.
	host := os.Getenv("DATAHUB_HOST")
	gms_token := os.Getenv("DATAHUB_GMS_TOKEN")

	if !config.Host.IsNull() {
		host = config.Host.ValueString()
	}

	if !config.GmsToken.IsNull() {
		gms_token = config.GmsToken.ValueString()
	}

	// Last resort: Datahub CLI local configuration at ~/.datahubenv
	if host == "" || gms_token == "" {
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
				server := strings.TrimSpace(envCfg.Gms.Server)
				server = strings.TrimRight(server, "/")
				server = strings.TrimSuffix(server, "/gms")
				host = server
			}
			if gms_token == "" && envCfg.Gms.Token != "" {
				gms_token = envCfg.Gms.Token
			}
		}
	}

	if host == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Missing Datahub API Host",
			"The provider cannot create the Datahub API client as there is a missing or empty value for the Datahub API host. "+
				"Set the host value in the configuration or use the DATAHUB_HOST environment variable. "+
				"If either is already set, ensure the value is not empty."+
				"Init otherwise datahub cli via `datahub init` command to create the configuration file at ~/.datahubenv",
		)
	}
	if gms_token == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("gms_token"),
			"Missing Datahub GMS Token",
			"The provider cannot create the Datahub API client as there is a missing or empty value for the Datahub GMS token. "+
				"Set the gms_token value in the configuration or use the DATAHUB_GMS_TOKEN environment variable. "+
				"If either is already set, ensure the value is not empty."+
				"Init otherwise datahub cli via `datahub init` command to create the configuration file at ~/.datahubenv",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Create a new Datahub client using the configuration values
	client, err := datahub.NewClient(host, gms_token)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Datahub API Client",
			"An unexpected error occurred when creating the Datahub API client. "+
				"If the error is not clear, please contact the provider developers.\n\n"+
				"Datahub Client Error: "+err.Error(),
		)
		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

// DataSources defines the data sources implemented in the provider.
func (p *datahubProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

// Resources defines the resources implemented in the provider.
func (p *datahubProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewIngestionSourceResource,
	}
}
