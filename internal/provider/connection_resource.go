// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ resource.Resource                = &connectionResource{}
	_ resource.ResourceWithConfigure   = &connectionResource{}
	_ resource.ResourceWithImportState = &connectionResource{}
)

type connectionResource struct {
	client *datahub.Client
}

// connectionResourceModel is the Terraform state model for datahub_connection.
// All per-platform config fields (inside nested blocks) are WriteOnly+Sensitive
// because the DataHub server encrypts the entire config blob at rest; no
// individual config field can be read back from the API.
//
// Only top-level entity metadata (name, platform) are drift-detected via the
// OpenAPI v3 Read path. Per-platform config is sent once on Create/Update and
// updated by bumping config_wo_version, which triggers a replacement.
type connectionResourceModel struct {
	ID              types.String       `tfsdk:"id"`
	URN             types.String       `tfsdk:"urn"`
	ConnectionID    types.String       `tfsdk:"connection_id"`
	Name            types.String       `tfsdk:"name"`
	Platform        types.String       `tfsdk:"platform"`
	ConfigWOVersion types.Int64        `tfsdk:"config_wo_version"`
	Databricks      *databricksModel   `tfsdk:"databricks"`
	Snowflake       *snowflakeModel    `tfsdk:"snowflake"`
	BigQuery        *bigqueryModel     `tfsdk:"bigquery"`
	Redshift        *redshiftModel     `tfsdk:"redshift"`
	UnityCatalog    *unityCatalogModel `tfsdk:"unity_catalog"`
	RawConfig       *rawConfigModel    `tfsdk:"raw_config"`
}

type databricksModel struct {
	WorkspaceURL        types.String `tfsdk:"workspace_url"`
	WarehouseID         types.String `tfsdk:"warehouse_id"`
	AuthType            types.String `tfsdk:"auth_type"`
	PersonalAccessToken types.String `tfsdk:"personal_access_token_wo"`
	ClientID            types.String `tfsdk:"client_id_wo"`
	ClientSecret        types.String `tfsdk:"client_secret_wo"`
}

type snowflakeModel struct {
	AccountID              types.String `tfsdk:"account_id"`
	Warehouse              types.String `tfsdk:"warehouse"`
	Database               types.String `tfsdk:"database"`
	Role                   types.String `tfsdk:"role"`
	AuthType               types.String `tfsdk:"auth_type"`
	PasswordWO             types.String `tfsdk:"password_wo"`
	PrivateKeyWO           types.String `tfsdk:"private_key_wo"`
	PrivateKeyPassphraseWO types.String `tfsdk:"private_key_passphrase_wo"`
}

type bigqueryModel struct {
	ProjectID      types.String `tfsdk:"project_id"`
	PrivateKeyJSON types.String `tfsdk:"private_key_json_wo"`
}

type redshiftModel struct {
	HostPort   types.String `tfsdk:"host_port"`
	Database   types.String `tfsdk:"database"`
	Username   types.String `tfsdk:"username"`
	PasswordWO types.String `tfsdk:"password_wo"`
}

type unityCatalogModel struct {
	WorkspaceURL        types.String `tfsdk:"workspace_url"`
	WarehouseID         types.String `tfsdk:"warehouse_id"`
	AuthType            types.String `tfsdk:"auth_type"`
	PersonalAccessToken types.String `tfsdk:"personal_access_token_wo"`
	ClientID            types.String `tfsdk:"client_id_wo"`
	ClientSecret        types.String `tfsdk:"client_secret_wo"`
}

// rawConfigModel is the escape hatch for platforms without a typed block.
// platform_urn_suffix is the DataHub platform name (e.g., "looker") used to
// construct the platform URN. config_json_wo is the full platform config as a
// JSON string -- callers are responsible for constructing the correct schema
// for their platform.
type rawConfigModel struct {
	PlatformURNSuffix types.String `tfsdk:"platform_urn_suffix"`
	ConfigJSON        types.String `tfsdk:"config_json_wo"`
}

func NewConnectionResource() resource.Resource {
	return &connectionResource{}
}

func (r *connectionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *connectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connection"
}

func (r *connectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a DataHub Connection.\n\n" +
			"DataHub Connections are named, encrypted credential configurations for data platforms " +
			"(Databricks, Snowflake, BigQuery, Redshift, Unity Catalog) and are displayed on the " +
			"DataHub Cloud Integrations page. A connection can be referenced in an ingestion source " +
			"recipe via the `connection` field (e.g., `connection: urn:li:dataHubConnection:my-conn`).\n\n" +
			"## Security model\n\n" +
			"DataHub encrypts the entire connection config blob server-side (AES-GCM-256) before " +
			"persisting it. The encrypted blob is never returned by the read API. As a result:\n\n" +
			"- All fields inside the platform config blocks are **WriteOnly**: they are sent to " +
			"DataHub on create/update but are not stored in Terraform state.\n" +
			"- Only top-level metadata (`name`, `platform`) is drift-detected via the strongly-consistent " +
			"OpenAPI v3 read path.\n\n" +
			"**Requires Terraform CLI 1.11 or later** (WriteOnly attribute support).\n\n" +
			"## Credential rotation\n\n" +
			"Because all config fields are WriteOnly, Terraform cannot detect per-field drift on its own. " +
			"To update any connection config (credentials or other settings), increment " +
			"`config_wo_version` (e.g., from `1` to `2`). Terraform will plan a replacement -- " +
			"deleting and recreating the connection with the updated config from your current configuration.\n\n" +
			"## Platform config blocks\n\n" +
			"Exactly one platform block must be configured. Use the block matching your data platform " +
			"(`databricks`, `snowflake`, `bigquery`, `redshift`, `unity_catalog`). For platforms " +
			"without a dedicated block, use `raw_config` and supply the JSON directly.\n\n" +
			"### Databricks field reference\n\n" +
			"| Field | Description |\n" +
			"|---|---|\n" +
			"| `workspace_url` | Databricks workspace URL (e.g., `https://dbc-xxx.cloud.databricks.com`) |\n" +
			"| `warehouse_id` | SQL Warehouse ID |\n" +
			"| `auth_type` | `PERSONAL_ACCESS_TOKEN` or `OAUTH_M2M` |\n" +
			"| `personal_access_token_wo` | PAT credential (required when `auth_type = PERSONAL_ACCESS_TOKEN`) |\n" +
			"| `client_id_wo` | OAuth client ID (required when `auth_type = OAUTH_M2M`) |\n" +
			"| `client_secret_wo` | OAuth client secret (required when `auth_type = OAUTH_M2M`) |\n\n" +
			"### Snowflake field reference\n\n" +
			"| Field | Description |\n" +
			"|---|---|\n" +
			"| `account_id` | Snowflake account identifier (e.g., `xy12345.us-east-1`) |\n" +
			"| `warehouse` | Snowflake warehouse name |\n" +
			"| `database` | Default database |\n" +
			"| `role` | Snowflake role |\n" +
			"| `auth_type` | `USER_PASS` or `KEY_PAIR` |\n" +
			"| `password_wo` | Password (required when `auth_type = USER_PASS`) |\n" +
			"| `private_key_wo` | PEM-encoded private key (required when `auth_type = KEY_PAIR`) |\n" +
			"| `private_key_passphrase_wo` | Private key passphrase (optional, for encrypted keys) |\n\n" +
			"### BigQuery field reference\n\n" +
			"| Field | Description |\n" +
			"|---|---|\n" +
			"| `project_id` | GCP project ID |\n" +
			"| `private_key_json_wo` | Full service account JSON key file contents |\n\n" +
			"### Redshift field reference\n\n" +
			"| Field | Description |\n" +
			"|---|---|\n" +
			"| `host_port` | Redshift endpoint and port (e.g., `cluster.region.redshift.amazonaws.com:5439`) |\n" +
			"| `database` | Database name |\n" +
			"| `username` | Redshift username |\n" +
			"| `password_wo` | Redshift password |\n\n" +
			"### Unity Catalog field reference\n\n" +
			"Identical to the `databricks` block but targets the Unity Catalog platform URN.\n\n" +
			"## Post-import note\n\n" +
			"After `terraform import`, only `name` and `platform` are populated from DataHub " +
			"(the config blob is encrypted and unavailable). You must add a matching platform " +
			"block in your configuration and set `config_wo_version` before the next apply.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this connection (e.g., `urn:li:dataHubConnection:prod-databricks`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"connection_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Unique identifier for this connection. Becomes the URN suffix (`urn:li:dataHubConnection:<connection_id>`). Must be URL-safe. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable display name for the connection, shown in the DataHub Integrations UI.",
			},
			"platform": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "DataHub platform identifier (e.g., `databricks`, `snowflake`). Derived from the configured platform block. Drift-detected: if the platform changes outside Terraform, the next plan will surface it.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"config_wo_version": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Rotation counter for the connection config. Increment this integer (e.g., `1` -> `2`) to trigger a replacement of the connection with the updated config from your current configuration. The integer itself is arbitrary; only changes to it matter. **Requires Terraform CLI 1.11+.**",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplaceIfConfigured(),
				},
			},
		},
		Blocks: map[string]schema.Block{
			"databricks": schema.SingleNestedBlock{
				MarkdownDescription: "Configuration for a Databricks connection. See field reference in the resource description.",
				Attributes: map[string]schema.Attribute{
					"workspace_url": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Databricks workspace URL (e.g., `https://dbc-39f83129-3f92.cloud.databricks.com`).",
					},
					"warehouse_id": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "SQL Warehouse ID.",
					},
					"auth_type": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Authentication type: `PERSONAL_ACCESS_TOKEN` or `OAUTH_M2M`.",
					},
					"personal_access_token_wo": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Personal access token. Required when `auth_type = PERSONAL_ACCESS_TOKEN`.",
					},
					"client_id_wo": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "OAuth M2M client ID. Required when `auth_type = OAUTH_M2M`.",
					},
					"client_secret_wo": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "OAuth M2M client secret. Required when `auth_type = OAUTH_M2M`.",
					},
				},
			},
			"snowflake": schema.SingleNestedBlock{
				MarkdownDescription: "Configuration for a Snowflake connection. See field reference in the resource description.",
				Attributes: map[string]schema.Attribute{
					"account_id": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Snowflake account identifier (e.g., `xy12345.us-east-1`).",
					},
					"warehouse": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Snowflake warehouse name.",
					},
					"database": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Default database name.",
					},
					"role": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Snowflake role. Defaults to the user's default role when omitted.",
					},
					"auth_type": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Authentication type: `USER_PASS` or `KEY_PAIR`.",
					},
					"password_wo": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Snowflake password. Required when `auth_type = USER_PASS`.",
					},
					"private_key_wo": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "PEM-encoded private key. Required when `auth_type = KEY_PAIR`.",
					},
					"private_key_passphrase_wo": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Private key passphrase for encrypted private keys. Optional.",
					},
				},
			},
			"bigquery": schema.SingleNestedBlock{
				MarkdownDescription: "Configuration for a BigQuery connection. See field reference in the resource description.",
				Attributes: map[string]schema.Attribute{
					"project_id": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "GCP project ID.",
					},
					"private_key_json_wo": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Full service account JSON key file contents (the JSON string from the downloaded key file).",
					},
				},
			},
			"redshift": schema.SingleNestedBlock{
				MarkdownDescription: "Configuration for a Redshift connection. See field reference in the resource description.",
				Attributes: map[string]schema.Attribute{
					"host_port": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Redshift endpoint and port (e.g., `cluster.region.redshift.amazonaws.com:5439`).",
					},
					"database": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Database name.",
					},
					"username": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Redshift username.",
					},
					"password_wo": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Redshift password.",
					},
				},
			},
			"unity_catalog": schema.SingleNestedBlock{
				MarkdownDescription: "Configuration for a Databricks Unity Catalog connection. Fields are identical to the `databricks` block but target the `unity-catalog` platform URN.",
				Attributes: map[string]schema.Attribute{
					"workspace_url": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Databricks workspace URL.",
					},
					"warehouse_id": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "SQL Warehouse ID.",
					},
					"auth_type": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Authentication type: `PERSONAL_ACCESS_TOKEN` or `OAUTH_M2M`.",
					},
					"personal_access_token_wo": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Personal access token. Required when `auth_type = PERSONAL_ACCESS_TOKEN`.",
					},
					"client_id_wo": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "OAuth M2M client ID. Required when `auth_type = OAUTH_M2M`.",
					},
					"client_secret_wo": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "OAuth M2M client secret. Required when `auth_type = OAUTH_M2M`.",
					},
				},
			},
			"raw_config": schema.SingleNestedBlock{
				MarkdownDescription: "Generic escape hatch for platforms without a dedicated typed block. Supply the platform identifier and the full config JSON directly.",
				Attributes: map[string]schema.Attribute{
					"platform_urn_suffix": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "DataHub platform identifier (the URN suffix, e.g., `looker`, `tableau`). Used to construct `urn:li:dataPlatform:<platform_urn_suffix>`.",
					},
					"config_json_wo": schema.StringAttribute{
						Optional:            true,
						WriteOnly:           true,
						Sensitive:           true,
						MarkdownDescription: "Full platform config serialized as a JSON string. The schema depends on the platform; consult the DataHub source for the expected fields.",
					},
				},
			},
		},
	}
}

// platformFromModel returns the platform URN suffix (e.g., "databricks") derived
// from whichever nested block is configured in the model. Returns "" if no block
// is configured (validation should have caught this earlier).
func platformFromModel(m *connectionResourceModel) string {
	switch {
	case m.Databricks != nil:
		return "databricks"
	case m.Snowflake != nil:
		return "snowflake"
	case m.BigQuery != nil:
		return "bigquery"
	case m.Redshift != nil:
		return "redshift"
	case m.UnityCatalog != nil:
		return "unity-catalog"
	case m.RawConfig != nil:
		return m.RawConfig.PlatformURNSuffix.ValueString()
	}
	return ""
}

// requireField returns an error if the given types.String value is null or empty.
func requireField(val types.String, block, field string) error {
	if val.IsNull() || val.ValueString() == "" {
		return fmt.Errorf("%s.%s is required when the %s block is configured", block, field, block)
	}
	return nil
}

// marshalBlob serializes the configured platform block into a JSON string
// suitable for sending to DataHub's upsertConnection mutation. Must be called
// with values from req.Config (not req.Plan) since all block attrs are WriteOnly.
// Returns an error if required fields within the chosen block are missing.
func marshalBlob(m *connectionResourceModel) (string, error) {
	var blob map[string]any

	switch {
	case m.Databricks != nil:
		d := m.Databricks
		for _, chk := range []struct {
			v types.String
			f string
		}{
			{d.WorkspaceURL, "workspace_url"},
			{d.WarehouseID, "warehouse_id"},
			{d.AuthType, "auth_type"},
		} {
			if err := requireField(chk.v, "databricks", chk.f); err != nil {
				return "", err
			}
		}
		b := map[string]any{
			"workspace_url": d.WorkspaceURL.ValueString(),
			"warehouse_id":  d.WarehouseID.ValueString(),
			"auth_type":     d.AuthType.ValueString(),
		}
		if !d.PersonalAccessToken.IsNull() && d.PersonalAccessToken.ValueString() != "" {
			b["personal_access_token"] = d.PersonalAccessToken.ValueString()
		}
		if !d.ClientID.IsNull() && d.ClientID.ValueString() != "" {
			b["client_id"] = d.ClientID.ValueString()
		}
		if !d.ClientSecret.IsNull() && d.ClientSecret.ValueString() != "" {
			b["client_secret"] = d.ClientSecret.ValueString()
		}
		blob = b

	case m.Snowflake != nil:
		s := m.Snowflake
		for _, chk := range []struct {
			v types.String
			f string
		}{
			{s.AccountID, "account_id"},
			{s.Warehouse, "warehouse"},
			{s.Database, "database"},
			{s.AuthType, "auth_type"},
		} {
			if err := requireField(chk.v, "snowflake", chk.f); err != nil {
				return "", err
			}
		}
		b := map[string]any{
			"account_id": s.AccountID.ValueString(),
			"warehouse":  s.Warehouse.ValueString(),
			"database":   s.Database.ValueString(),
			"auth_type":  s.AuthType.ValueString(),
		}
		if !s.Role.IsNull() && s.Role.ValueString() != "" {
			b["role"] = s.Role.ValueString()
		}
		if !s.PasswordWO.IsNull() && s.PasswordWO.ValueString() != "" {
			b["password"] = s.PasswordWO.ValueString()
		}
		if !s.PrivateKeyWO.IsNull() && s.PrivateKeyWO.ValueString() != "" {
			b["private_key"] = s.PrivateKeyWO.ValueString()
		}
		if !s.PrivateKeyPassphraseWO.IsNull() && s.PrivateKeyPassphraseWO.ValueString() != "" {
			b["private_key_passphrase"] = s.PrivateKeyPassphraseWO.ValueString()
		}
		blob = b

	case m.BigQuery != nil:
		for _, chk := range []struct {
			v types.String
			f string
		}{
			{m.BigQuery.ProjectID, "project_id"},
			{m.BigQuery.PrivateKeyJSON, "private_key_json_wo"},
		} {
			if err := requireField(chk.v, "bigquery", chk.f); err != nil {
				return "", err
			}
		}
		blob = map[string]any{
			"project_id":       m.BigQuery.ProjectID.ValueString(),
			"private_key_json": m.BigQuery.PrivateKeyJSON.ValueString(),
		}

	case m.Redshift != nil:
		for _, chk := range []struct {
			v types.String
			f string
		}{
			{m.Redshift.HostPort, "host_port"},
			{m.Redshift.Database, "database"},
			{m.Redshift.Username, "username"},
			{m.Redshift.PasswordWO, "password_wo"},
		} {
			if err := requireField(chk.v, "redshift", chk.f); err != nil {
				return "", err
			}
		}
		blob = map[string]any{
			"host_port": m.Redshift.HostPort.ValueString(),
			"database":  m.Redshift.Database.ValueString(),
			"username":  m.Redshift.Username.ValueString(),
			"password":  m.Redshift.PasswordWO.ValueString(),
		}

	case m.UnityCatalog != nil:
		uc := m.UnityCatalog
		for _, chk := range []struct {
			v types.String
			f string
		}{
			{uc.WorkspaceURL, "workspace_url"},
			{uc.WarehouseID, "warehouse_id"},
			{uc.AuthType, "auth_type"},
		} {
			if err := requireField(chk.v, "unity_catalog", chk.f); err != nil {
				return "", err
			}
		}
		b := map[string]any{
			"workspace_url": uc.WorkspaceURL.ValueString(),
			"warehouse_id":  uc.WarehouseID.ValueString(),
			"auth_type":     uc.AuthType.ValueString(),
		}
		if !uc.PersonalAccessToken.IsNull() && uc.PersonalAccessToken.ValueString() != "" {
			b["personal_access_token"] = uc.PersonalAccessToken.ValueString()
		}
		if !uc.ClientID.IsNull() && uc.ClientID.ValueString() != "" {
			b["client_id"] = uc.ClientID.ValueString()
		}
		if !uc.ClientSecret.IsNull() && uc.ClientSecret.ValueString() != "" {
			b["client_secret"] = uc.ClientSecret.ValueString()
		}
		blob = b

	case m.RawConfig != nil:
		for _, chk := range []struct {
			v types.String
			f string
		}{
			{m.RawConfig.PlatformURNSuffix, "platform_urn_suffix"},
			{m.RawConfig.ConfigJSON, "config_json_wo"},
		} {
			if err := requireField(chk.v, "raw_config", chk.f); err != nil {
				return "", err
			}
		}
		return m.RawConfig.ConfigJSON.ValueString(), nil

	default:
		return "", fmt.Errorf("no platform block configured")
	}

	b, err := json.Marshal(blob)
	if err != nil {
		return "", fmt.Errorf("marshaling connection config: %w", err)
	}
	return string(b), nil
}

// validateExactlyOneBlock checks that exactly one platform block is configured
// and adds a diagnostic error if not.
func validateExactlyOneBlock(m *connectionResourceModel, diags interface {
	AddError(summary, detail string)
}) bool {
	count := 0
	if m.Databricks != nil {
		count++
	}
	if m.Snowflake != nil {
		count++
	}
	if m.BigQuery != nil {
		count++
	}
	if m.Redshift != nil {
		count++
	}
	if m.UnityCatalog != nil {
		count++
	}
	if m.RawConfig != nil {
		count++
	}

	if count == 0 {
		diags.AddError(
			"No platform block configured",
			"Exactly one platform block must be configured: databricks, snowflake, bigquery, redshift, unity_catalog, or raw_config.",
		)
		return false
	}
	if count > 1 {
		diags.AddError(
			"Multiple platform blocks configured",
			"Exactly one platform block must be configured. Remove the extra blocks.",
		)
		return false
	}
	return true
}

// nullBlockForPlatform returns an all-null nested block struct matching the
// given platform. Used in ImportState to reconstruct state shape without
// exposing any config values (which are unreadable due to server-side encryption).
func nullBlockForPlatform(platform string, state *connectionResourceModel) {
	switch platform {
	case "databricks":
		state.Databricks = &databricksModel{
			WorkspaceURL:        types.StringNull(),
			WarehouseID:         types.StringNull(),
			AuthType:            types.StringNull(),
			PersonalAccessToken: types.StringNull(),
			ClientID:            types.StringNull(),
			ClientSecret:        types.StringNull(),
		}
	case "snowflake":
		state.Snowflake = &snowflakeModel{
			AccountID:              types.StringNull(),
			Warehouse:              types.StringNull(),
			Database:               types.StringNull(),
			Role:                   types.StringNull(),
			AuthType:               types.StringNull(),
			PasswordWO:             types.StringNull(),
			PrivateKeyWO:           types.StringNull(),
			PrivateKeyPassphraseWO: types.StringNull(),
		}
	case "bigquery":
		state.BigQuery = &bigqueryModel{
			ProjectID:      types.StringNull(),
			PrivateKeyJSON: types.StringNull(),
		}
	case "redshift":
		state.Redshift = &redshiftModel{
			HostPort:   types.StringNull(),
			Database:   types.StringNull(),
			Username:   types.StringNull(),
			PasswordWO: types.StringNull(),
		}
	case "unity-catalog":
		state.UnityCatalog = &unityCatalogModel{
			WorkspaceURL:        types.StringNull(),
			WarehouseID:         types.StringNull(),
			AuthType:            types.StringNull(),
			PersonalAccessToken: types.StringNull(),
			ClientID:            types.StringNull(),
			ClientSecret:        types.StringNull(),
		}
	default:
		// Unknown platform: use raw_config so the user knows what to fill in.
		state.RawConfig = &rawConfigModel{
			PlatformURNSuffix: types.StringNull(),
			ConfigJSON:        types.StringNull(),
		}
	}
}

func (r *connectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan connectionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// WriteOnly attributes are null in the plan; read them from the request config.
	var config connectionResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !validateExactlyOneBlock(&config, &resp.Diagnostics) {
		return
	}

	platform := platformFromModel(&config)
	blob, err := marshalBlob(&config)
	if err != nil {
		resp.Diagnostics.AddError("Config serialization error", err.Error())
		return
	}

	connID := plan.ConnectionID.ValueString()
	urn := "urn:li:dataHubConnection:" + connID

	returnedURN, err := r.client.UpsertConnection(ctx, datahub.UpsertConnectionInput{
		ID:       connID,
		Name:     plan.Name.ValueString(),
		Platform: platform,
		Blob:     blob,
	})
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if returnedURN != "" {
		urn = returnedURN
	}

	plan.URN = types.StringValue(urn)
	plan.ID = types.StringValue(urn)
	plan.Platform = types.StringValue(platform)
	// All block attrs are WriteOnly -- the Framework nullifies them in state automatically.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *connectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state connectionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}

	conn, err := r.client.GetConnectionByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if conn == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	// Only update top-level metadata that is readable from the entity endpoint.
	// Block attrs (the config blob) are encrypted at rest and cannot be read back.
	state.URN = types.StringValue(conn.URN)
	state.ID = types.StringValue(conn.URN)
	state.Name = types.StringValue(conn.Name)
	state.Platform = types.StringValue(conn.Platform)
	// config_wo_version and all block attrs keep their state values unchanged.

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *connectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan connectionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state connectionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// WriteOnly attributes are null in the plan; read them from the request config.
	var config connectionResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !validateExactlyOneBlock(&config, &resp.Diagnostics) {
		return
	}

	platform := platformFromModel(&config)
	blob, err := marshalBlob(&config)
	if err != nil {
		resp.Diagnostics.AddError("Config serialization error", err.Error())
		return
	}

	connID := state.ConnectionID.ValueString()

	_, err = r.client.UpsertConnection(ctx, datahub.UpsertConnectionInput{
		ID:       connID,
		Name:     plan.Name.ValueString(),
		Platform: platform,
		Blob:     blob,
	})
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.URN = state.URN
	plan.ID = state.ID
	plan.Platform = types.StringValue(platform)
	// All block attrs are WriteOnly -- the Framework nullifies them in state automatically.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *connectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state connectionResourceModel
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

	if err := r.client.DeleteConnection(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *connectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub connection URN (e.g., urn:li:dataHubConnection:prod-databricks) or a bare connection ID.")
		return
	}

	const urnPrefix = "urn:li:dataHubConnection:"
	var connID, urn string
	if strings.HasPrefix(raw, urnPrefix) {
		urn = raw
		connID = strings.TrimPrefix(raw, urnPrefix)
	} else {
		connID = raw
		urn = urnPrefix + connID
	}

	conn, err := r.client.GetConnectionByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if conn == nil {
		resp.Diagnostics.AddError(
			"Connection not found",
			fmt.Sprintf("No connection with URN %q was found in DataHub. Verify the ID or URN and retry.", urn),
		)
		return
	}
	if conn.ID != "" {
		connID = conn.ID
	}

	state := connectionResourceModel{
		ID:           types.StringValue(urn),
		URN:          types.StringValue(urn),
		ConnectionID: types.StringValue(connID),
		Name:         types.StringValue(conn.Name),
		Platform:     types.StringValue(conn.Platform),
		// config_wo_version is not available from the server; user must set it in config.
		ConfigWOVersion: types.Int64Null(),
	}

	// Set the appropriate null block so state shape matches a normal apply.
	// All block attrs are WriteOnly (null); only the block's presence/absence
	// reflects which platform was configured.
	nullBlockForPlatform(conn.Platform, &state)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)

	resp.Diagnostics.AddWarning(
		"Connection config not imported",
		"The DataHub connection config blob is encrypted at rest and cannot be read back from the API. "+
			"After import, you must add the appropriate platform block to your configuration with "+
			"the correct credentials and set config_wo_version before running terraform apply.",
	)
}
