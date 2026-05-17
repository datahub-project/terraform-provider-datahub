// Copyright 2026 The DataHub Project Authors
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/tools/uid"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource = &recipeDynamoDBDocumentDataSource{}
)

const dynamodbSourceType = "dynamodb"

// recipeDynamoDBDocumentDataSource renders a recipe JSON document for DynamoDB ingestion.
// Like the Unity recipe data source, it is intentionally flexible: source_config is a dynamic
// object and the only enforced inputs are source_type and source_config.
type recipeDynamoDBDocumentDataSource struct{}

type recipeDynamoDBDocumentModel struct {
	ID           types.String  `tfsdk:"id"`
	SourceID     types.String  `tfsdk:"source_id"`
	SourceType   types.String  `tfsdk:"source_type"`
	SourceConfig types.Dynamic `tfsdk:"source_config"`
	PipelineName types.String  `tfsdk:"pipeline_name"`
	JSON         types.String  `tfsdk:"json"`
}

type recipeDynamoDBDocumentPayload struct {
	Source struct {
		Type   string `json:"type"`
		Config any    `json:"config"`
	} `json:"source"`
	PipelineName string `json:"pipeline_name"`
}

func NewRecipeDynamoDBDocumentDataSource() datasource.DataSource {
	return &recipeDynamoDBDocumentDataSource{}
}

func (d *recipeDynamoDBDocumentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_recipe_dynamodb_document"
}

func (d *recipeDynamoDBDocumentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Builds a DataHub ingestion recipe JSON document for the `datahub_ingest` resource.\n\n" +
			"You provide structured inputs and get back a JSON string.\n\n" +
			"## Example Usage\n\n" +
			"```terraform\n" +
			"data \"datahub_recipe_dynamodb_document\" \"example\" {\n" +
			"  # source_id is optional; if omitted it is computed by hashing from source_type + source_config\n" +
			"  # source_id = \"example-dynamodb-source\"\n" +
			"  # source_type is optional and defaults to \"" + dynamodbSourceType + "\"\n\n" +
			"  # pipeline_name defaults to \"<source_type>:<source_id>\"\n" +
			"  # pipeline_name = \"" + dynamodbSourceType + ":example-dynamodb-source\"\n\n" +
			"  # NOTE: Prefer DataHub Secrets / env vars instead of raw credentials.\n" +
			"  source_config = {\n" +
			"    # Credentials are optional; if omitted the connector can use AWS default credential discovery\n" +
			"    # (env vars, profile, IAM role, etc).\n" +
			"    # aws_access_key_id     = \"$${AWS_ACCESS_KEY_ID}\"\n" +
			"    # aws_secret_access_key = \"$${AWS_SECRET_ACCESS_KEY}\"\n" +
			"    aws_region            = \"<my-aws-region>\"\n" +
			"    env                   = \"STG\"\n\n" +
			"    # Optional patterns\n" +
			"    database_pattern = {\n" +
			"      allow = [\"<my-aws-region>.<my-database>.*\"]\n" +
			"    }\n\n" +
			"    table_pattern = {\n" +
			"      allow = [\"<my-aws-region>.<my-database>.<my-table>.*\"]\n" +
			"    }\n\n" +
			"    domain = {\n" +
			"      my_datahub_domain = {\n" +
			"        allow = [\".*\"]\n" +
			"      }\n" +
			"    }\n\n" +
			"    stateful_ingestion = {\n" +
			"      enabled             = true\n" +
			"      fail_safe_threshold = 85\n" +
			"    }\n\n" +
			"    # Optional capabilities\n" +
			"    extract_table_tags = false\n" +
			"  }\n" +
			"}\n\n" +
			"resource \"datahub_ingest\" \"example\" {\n" +
			"  source_id   = \"example-dynamodb-source\"\n" +
			"  source_name = \"Example DynamoDB Source\"\n" +
			"  recipe = data.datahub_recipe_dynamodb_document.example.json\n" +
			"}\n" +
			"```\n\n" +
			"## Argument Reference\n\n" +
			"- `source_id` (Optional) Ingestion source identifier. Used to default `pipeline_name` when omitted and commonly reused as `datahub_ingest.source_id`. If omitted, the provider computes a stable value by hashing `source_type` + `source_config` and returns it in state.\n" +
			"- `source_type` (Optional) Must be `" + dynamodbSourceType + "`. If omitted, defaults to `" + dynamodbSourceType + "`.\n" +
			"- `source_config` (Required) A map/object that is embedded verbatim under `source.config` in the recipe JSON. The provider does not validate fields; it simply serializes the object.\n" +
			"- `pipeline_name` (Optional) Defaults to `<source_type>:<source_id>`.\n\n" +
			"## Notes\n\n" +
			"The upstream connector requires `aws_region` (breaking change starting in DataHub ingestion v0.13.3).\n\n" +
			"DynamoDB recipe options: https://docs.datahub.com/docs/generated/ingestion/sources/dynamodb",
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
				MarkdownDescription: "Recipe source type (the DataHub ingestion source type). For this data source it must be `" + dynamodbSourceType + "`. If omitted, defaults to `" + dynamodbSourceType + "`.",
			},
			"source_config": schema.DynamicAttribute{
				Required:            true,
				MarkdownDescription: "Arbitrary source config object embedded under `source.config`. This is serialized as-is into JSON (no field validation). For DynamoDB fields, see https://docs.datahub.com/docs/generated/ingestion/sources/dynamodb. AWS credentials are optional and can be omitted to use default AWS credential discovery. Prefer using `$${SECRET_NAME}` or `$${ENV_VAR}` placeholders for sensitive values.",
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

func (d *recipeDynamoDBDocumentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config recipeDynamoDBDocumentModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sourceType := dynamodbSourceType
	if !config.SourceType.IsUnknown() && !config.SourceType.IsNull() {
		v := strings.TrimSpace(config.SourceType.ValueString())
		if v != "" {
			sourceType = v
		}
	}
	if strings.ToLower(sourceType) != dynamodbSourceType {
		resp.Diagnostics.AddError("Invalid source_type", fmt.Sprintf("source_type must be %q", dynamodbSourceType))
		return
	}
	sourceType = dynamodbSourceType
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
		derived, err := deriveRecipeDynamoDBSourceID(sourceType, configAny)
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

	var payload recipeDynamoDBDocumentPayload
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

func deriveRecipeDynamoDBSourceID(sourceType string, sourceConfig any) (string, error) {
	configJSON, err := json.Marshal(sourceConfig)
	if err != nil {
		return "", err
	}

	hashInput := append([]byte(sourceType+"\n"), configJSON...)
	return uid.DeriveID(sourceType, hashInput, 0), nil
}
