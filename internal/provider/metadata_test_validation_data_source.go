// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

var (
	_ datasource.DataSource              = &metadataTestValidationDataSource{}
	_ datasource.DataSourceWithConfigure = &metadataTestValidationDataSource{}
)

type metadataTestValidationDataSource struct {
	client *datahub.Client
}

type metadataTestValidationDataSourceModel struct {
	DefinitionJSON jsontypes.Normalized `tfsdk:"definition_json"`
}

// NewMetadataTestValidationDataSource returns the
// datahub_metadata_test_validation data source.
func NewMetadataTestValidationDataSource() datasource.DataSource {
	return &metadataTestValidationDataSource{}
}

func (d *metadataTestValidationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_metadata_test_validation"
}

func (d *metadataTestValidationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: cloudOnlyBadge +
			"Validates a metadata test definition against the DataHub Cloud test engine (the " +
			"`validateTest` GraphQL query) **without persisting anything**, and fails with the " +
			"server's validation messages when the definition is invalid.\n\n" +
			"Terraform reads data sources during `terraform plan` when their configuration is fully " +
			"known, so wiring a definition through this data source surfaces a malformed rule " +
			"document at plan time instead of at apply. Thread the `definition_json` output into " +
			"the `datahub_metadata_test` resource so the dependency (and the validation) is " +
			"ordered ahead of the write:\n\n" +
			"```hcl\n" +
			"data \"datahub_metadata_test_validation\" \"check\" {\n" +
			"  definition_json = local.definition\n" +
			"}\n\n" +
			"resource \"datahub_metadata_test\" \"example\" {\n" +
			"  # ...\n" +
			"  definition_json = data.datahub_metadata_test_validation.check.definition_json\n" +
			"}\n" +
			"```\n\n" +
			"Two costs are inherent to this pattern, which is why it is opt-in rather than built " +
			"into the resource: every plan makes a network call to DataHub (so plans fail without " +
			"connectivity and credentials), and a definition that is not fully known at plan time " +
			"(fed from another resource's computed attribute) defers the read -- and the validation " +
			"-- to apply. `datahub_metadata_test` itself needs no help on DataHub Cloud at apply " +
			"time: `createTest`/`updateTest` run the same server-side validation and fail with the " +
			"same messages.\n\n" +
			"DataHub Cloud only: OSS DataHub has no `validateTest` query (and no test engine to " +
			"validate against), so this data source fails there with a clear error.",
		Attributes: map[string]schema.Attribute{
			"definition_json": schema.StringAttribute{
				Required:   true,
				CustomType: jsontypes.NormalizedType{},
				MarkdownDescription: "Metadata test definition JSON to validate. On success it is echoed back " +
					"unchanged, so it can be referenced by a `datahub_metadata_test` resource to order the " +
					"validation ahead of the write.",
			},
		},
	}
}

func (d *metadataTestValidationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*datahub.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *datahub.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	d.client = client
}

func (d *metadataTestValidationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config metadataTestValidationDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	definition := config.DefinitionJSON.ValueString()
	valid, messages, err := d.client.ValidateMetadataTestDefinition(ctx, definition)
	if err != nil {
		if errors.Is(err, datahub.ErrMetadataTestValidationCloudOnly) {
			resp.Diagnostics.AddError("DataHub Cloud Required",
				"datahub_metadata_test_validation requires DataHub Cloud. "+
					"The configured DataHub instance does not expose the validateTest query "+
					"(OSS DataHub has no test engine to validate against).")
			return
		}
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if !valid {
		detail := "DataHub rejected the metadata test definition."
		if len(messages) > 0 {
			detail += "\n\n" + strings.Join(messages, "\n")
		}
		resp.Diagnostics.AddError("Invalid metadata test definition", detail)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
