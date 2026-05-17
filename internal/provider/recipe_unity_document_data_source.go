// Copyright 2026 The DataHub Project Authors
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/datahub/terraform-provider-datahub/internal/provider/pkg/tools/uid"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var (
	_ datasource.DataSource = &recipeUnityDocumentDataSource{}
)

const unityCatalogSourceType = "unity-catalog"

// recipeUnityDocumentDataSource renders a recipe JSON document for Unity ingestion.
// Despite the name, it is intentionally flexible: source_config is a dynamic object and
// the only enforced inputs are source_type and source_config.
type recipeUnityDocumentDataSource struct{}

type recipeUnityDocumentModel struct {
	ID           types.String  `tfsdk:"id"`
	SourceID     types.String  `tfsdk:"source_id"`
	SourceType   types.String  `tfsdk:"source_type"`
	SourceConfig types.Dynamic `tfsdk:"source_config"`
	PipelineName types.String  `tfsdk:"pipeline_name"`
	JSON         types.String  `tfsdk:"json"`
}

type recipeUnityDocumentPayload struct {
	Source struct {
		Type   string `json:"type"`
		Config any    `json:"config"`
	} `json:"source"`
	PipelineName string `json:"pipeline_name"`
}

func NewRecipeUnityDocumentDataSource() datasource.DataSource {
	return &recipeUnityDocumentDataSource{}
}

func (d *recipeUnityDocumentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_recipe_unity_document"
}

func (d *recipeUnityDocumentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Builds a DataHub ingestion recipe JSON document for the `datahub_ingest` resource.\n\n" +
			"You provide structured inputs and get back a JSON string.\n\n" +
			"## Example Usage\n\n" +
			"```terraform\n" +
			"data \"datahub_recipe_unity_document\" \"example\" {\n" +
			"  # source_id is optional; if omitted it is computed by hashing from source_type + source_config\n" +
			"  # source_id = \"example-unity-source\"\n" +
			"  # source_type is optional and defaults to \"" + unityCatalogSourceType + "\"\n" +
			"\n" +
			"  # pipeline_name defaults to \"<source_type>:<source_id>\"\n" +
			"  # pipeline_name = \"" + unityCatalogSourceType + ":example-unity-source\"\n" +
			"\n" +
			"  # NOTE: Prefer DataHub Secrets / env vars instead of raw credentials.\n" +
			"  source_config = {\n" +
			"    workspace_url = \"https://adb-<workspace>.azuredatabricks.net\"\n" +
			"    env           = \"<ENV>\"\n" +
			"    token         = \"$${DATABRICKS_TOKEN}\"\n" +
			"    warehouse_id  = \"<warehouse_id>\"\n" +
			"    catalogs      = [\"<catalog>\"]\n" +
			"\n" +
			"    schema_pattern = {\n" +
			"      allow = [\"<catalog>.<schema>.*\"]\n" +
			"      deny  = [\"information_schema\"]\n" +
			"    }\n" +
			"\n" +
			"    table_pattern = {\n" +
			"      allow = [\"<catalog>.<schema>.*\"]\n" +
			"    }\n" +
			"\n" +
			"    include_ownership      = true\n" +
			"    include_table_lineage  = true\n" +
			"    include_column_lineage = true\n" +
			"    lineage_data_source    = \"API\"\n" +
			"\n" +
			"    profiling = {\n" +
			"      enabled       = true\n" +
			"      method        = \"ge\"\n" +
			"      max_wait_secs = 60\n" +
			"    }\n" +
			"\n" +
			"    stateful_ingestion = {\n" +
			"      enabled             = true\n" +
			"      fail_safe_threshold = 85\n" +
			"    }\n" +
			"  }\n" +
			"}\n" +
			"\n" +
			"resource \"datahub_ingest\" \"example\" {\n" +
			"  source_id     = \"example-unity-source\"\n" +
			"  source_name   = \"example-unity-source\"\n" +
			"  cron_interval = \"0 10 * * *\"\n" +
			"  timezone      = \"UTC\"\n" +
			"  cli_version   = \"1.3.1.5\"\n" +
			"  async         = false\n" +
			"\n" +
			"  recipe = data.datahub_recipe_unity_document.example.json\n" +
			"}\n" +
			"```\n\n" +
			"## Argument Reference\n\n" +
			"- `source_id` (Optional) Ingestion source identifier. Used to default `pipeline_name` when omitted and commonly reused as `datahub_ingest.source_id`. If omitted, the provider computes a stable value by hashing `source_type` + `source_config` and returns it in state.\n" +
			"- `source_type` (Optional) Must be `" + unityCatalogSourceType + "`. If omitted, defaults to `" + unityCatalogSourceType + "`.\n" +
			"- `source_config` (Required) A map/object that is embedded verbatim under `source.config` in the recipe JSON. The provider does not validate fields; it simply serializes the object.\n" +
			"- `pipeline_name` (Optional) Defaults to `<source_type>:<source_id>`.\n\n" +
			"## Security Note\n\n" +
			"**Warning:** This data source renders whatever you put into `source_config` into the recipe JSON. If `source_config` contains credentials, they will be included in the recipe and can be stored in DataHub as part of the Ingestion Source configuration. This mirrors DataHub’s normal behavior when credentials are embedded into ingestion configs.\n\n" +
			"**Recommended:** Use DataHub Secrets and/or environment variable substitution in recipes (e.g. `\"token\": \"$${SECRET_NAME}\"`). DataHub expands environment variables in configs and supports secrets in UI ingestion. See https://docs.datahub.com/docs/ui-ingestion/#configuring-secrets and https://docs.datahub.com/docs/metadata-ingestion/recipe_overview#handling-sensitive-information-in-recipes.\n\n" +
			"Unity Catalog / Databricks recipe options: https://docs.datahub.com/docs/generated/ingestion/sources/databricks",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Stable identifier derived from the JSON output.",
			},
			"source_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Ingestion source id. Used to default `pipeline_name` when not set and commonly reused as `datahub_ingest.source_id`. If omitted, the provider computes a stable value by hashing `source_type` + `source_config` and returns it in state.",
			},
			"source_type": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Recipe source type (the DataHub ingestion source type). For this data source it must be `" + unityCatalogSourceType + "`. If omitted, defaults to `" + unityCatalogSourceType + "`.",
			},
			"source_config": schema.DynamicAttribute{
				Required:            true,
				MarkdownDescription: "Arbitrary source config object embedded under `source.config`. This is serialized as-is into JSON (no field validation). For Unity/Databricks fields, see https://docs.datahub.com/docs/generated/ingestion/sources/databricks. Prefer using `$${SECRET_NAME}` or `$${ENV_VAR}` placeholders for sensitive values.",
			},
			"pipeline_name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional pipeline name. If omitted, defaults to `<source_type>:<source_id>`.",
			},
			"json": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Rendered recipe JSON. If you included secrets inline in `source_config`, they will appear here.",
			},
		},
	}
}

func (d *recipeUnityDocumentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config recipeUnityDocumentModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sourceType := unityCatalogSourceType
	if !config.SourceType.IsUnknown() && !config.SourceType.IsNull() {
		v := strings.TrimSpace(config.SourceType.ValueString())
		if v != "" {
			sourceType = v
		}
	}
	if strings.ToLower(sourceType) != unityCatalogSourceType {
		resp.Diagnostics.AddError("Invalid source_type", fmt.Sprintf("source_type must be %q", unityCatalogSourceType))
		return
	}
	sourceType = unityCatalogSourceType
	config.SourceType = types.StringValue(sourceType)

	if config.SourceConfig.IsUnknown() || config.SourceConfig.IsNull() {
		resp.Diagnostics.AddError("Invalid source_config", "source_config must be a known, non-null value")
		return
	}

	configAny, diags := dynamicToAny(config.SourceConfig)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	sourceID := strings.TrimSpace(config.SourceID.ValueString())
	if config.SourceID.IsUnknown() || config.SourceID.IsNull() || sourceID == "" {
		derived, err := deriveRecipeUnitySourceID(sourceType, configAny)
		if err != nil {
			resp.Diagnostics.AddError("Unable to derive source_id", err.Error())
			return
		}
		sourceID = derived
	}
	config.SourceID = types.StringValue(sourceID)
	config.SourceType = types.StringValue(sourceType)

	pipelineName := strings.TrimSpace(config.PipelineName.ValueString())
	if config.PipelineName.IsUnknown() || config.PipelineName.IsNull() || pipelineName == "" {
		pipelineName = fmt.Sprintf("%s:%s", sourceType, sourceID)
	}
	config.PipelineName = types.StringValue(pipelineName)

	var payload recipeUnityDocumentPayload
	payload.Source.Type = sourceType
	payload.Source.Config = configAny
	payload.PipelineName = pipelineName

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		resp.Diagnostics.AddError("Unable to render recipe JSON", err.Error())
		return
	}

	config.ID = types.StringValue(uid.SHA256Hex(jsonBytes))
	config.JSON = types.StringValue(string(jsonBytes))

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

func deriveRecipeUnitySourceID(sourceType string, sourceConfig any) (string, error) {
	configJSON, err := json.Marshal(sourceConfig)
	if err != nil {
		return "", err
	}

	hashInput := append([]byte(sourceType+"\n"), configJSON...)
	return uid.DeriveID(sourceType, hashInput, 0), nil
}

// dynamicToAny converts a `types.Dynamic` to a Go value suitable for json.Marshal.
func dynamicToAny(v types.Dynamic) (any, diag.Diagnostics) {
	var diags diag.Diagnostics
	uv := v.UnderlyingValue()
	out, d := attrValueToAny(uv)
	diags.Append(d...)
	return out, diags
}

func attrValueToAny(v attr.Value) (any, diag.Diagnostics) {
	var diags diag.Diagnostics

	if v == nil {
		return nil, diags
	}

	// Unwrap nested DynamicValue.
	if dv, ok := v.(basetypes.DynamicValue); ok {
		if dv.IsNull() {
			return nil, diags
		}
		if dv.IsUnknown() {
			diags.AddError("Unknown value", "dynamic value is unknown")
			return nil, diags
		}
		return attrValueToAny(dv.UnderlyingValue())
	}

	switch tv := v.(type) {
	case basetypes.StringValue:
		if tv.IsNull() {
			return nil, diags
		}
		if tv.IsUnknown() {
			diags.AddError("Unknown value", "string value is unknown")
			return nil, diags
		}
		return tv.ValueString(), diags
	case basetypes.BoolValue:
		if tv.IsNull() {
			return nil, diags
		}
		if tv.IsUnknown() {
			diags.AddError("Unknown value", "bool value is unknown")
			return nil, diags
		}
		return tv.ValueBool(), diags
	case basetypes.Int64Value:
		if tv.IsNull() {
			return nil, diags
		}
		if tv.IsUnknown() {
			diags.AddError("Unknown value", "int64 value is unknown")
			return nil, diags
		}
		return tv.ValueInt64(), diags
	case basetypes.Float64Value:
		if tv.IsNull() {
			return nil, diags
		}
		if tv.IsUnknown() {
			diags.AddError("Unknown value", "float64 value is unknown")
			return nil, diags
		}
		return tv.ValueFloat64(), diags
	case basetypes.NumberValue:
		if tv.IsNull() {
			return nil, diags
		}
		if tv.IsUnknown() {
			diags.AddError("Unknown value", "number value is unknown")
			return nil, diags
		}
		// Preserve exactness and ensure it marshals as a JSON number.
		return json.Number(tv.String()), diags
	case basetypes.ListValue:
		if tv.IsNull() {
			return nil, diags
		}
		if tv.IsUnknown() {
			diags.AddError("Unknown value", "list value is unknown")
			return nil, diags
		}
		elems := tv.Elements()
		out := make([]any, 0, len(elems))
		for _, e := range elems {
			val, d := attrValueToAny(e)
			diags.Append(d...)
			out = append(out, val)
		}
		return out, diags
	case basetypes.TupleValue:
		if tv.IsNull() {
			return nil, diags
		}
		if tv.IsUnknown() {
			diags.AddError("Unknown value", "tuple value is unknown")
			return nil, diags
		}
		elems := tv.Elements()
		out := make([]any, 0, len(elems))
		for _, e := range elems {
			val, d := attrValueToAny(e)
			diags.Append(d...)
			out = append(out, val)
		}
		return out, diags
	case basetypes.MapValue:
		if tv.IsNull() {
			return nil, diags
		}
		if tv.IsUnknown() {
			diags.AddError("Unknown value", "map value is unknown")
			return nil, diags
		}
		elems := tv.Elements()
		out := make(map[string]any, len(elems))
		for k, e := range elems {
			val, d := attrValueToAny(e)
			diags.Append(d...)
			out[k] = val
		}
		return out, diags
	case basetypes.ObjectValue:
		if tv.IsNull() {
			return nil, diags
		}
		if tv.IsUnknown() {
			diags.AddError("Unknown value", "object value is unknown")
			return nil, diags
		}
		attrs := tv.Attributes()
		out := make(map[string]any, len(attrs))
		for k, e := range attrs {
			val, d := attrValueToAny(e)
			diags.Append(d...)
			out[k] = val
		}
		return out, diags
	default:
		diags.AddError("Unsupported value type", fmt.Sprintf("unsupported attribute value type: %T", v))
		return nil, diags
	}
}
