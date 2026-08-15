// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/tools/uid"
)

var (
	_ resource.Resource                = &metadataTestResource{}
	_ resource.ResourceWithConfigure   = &metadataTestResource{}
	_ resource.ResourceWithImportState = &metadataTestResource{}
)

type metadataTestResource struct {
	client *datahub.Client
}

type metadataTestResourceModel struct {
	ID             types.String         `tfsdk:"id"`
	URN            types.String         `tfsdk:"urn"`
	TestID         types.String         `tfsdk:"test_id"`
	Name           types.String         `tfsdk:"name"`
	Category       types.String         `tfsdk:"category"`
	Description    types.String         `tfsdk:"description"`
	DefinitionJSON jsontypes.Normalized `tfsdk:"definition_json"`
}

// NewMetadataTestResource returns a new datahub_metadata_test resource.
func NewMetadataTestResource() resource.Resource {
	return &metadataTestResource{}
}

func (r *metadataTestResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	r.client = pd.Client
}

func (r *metadataTestResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_metadata_test"
}

func (r *metadataTestResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a DataHub **metadata test** (the `test` entity) -- a declarative " +
			"governance rule, expressed as a JSON definition, that DataHub evaluates against catalog " +
			"entities. Example: \"every PROD dataset must have an owner\".\n\n" +
			"The entity type and its create/update/delete API exist on both OSS DataHub and DataHub " +
			"Cloud, but the **evaluation engine is DataHub Cloud only**: on OSS the definition is " +
			"stored verbatim (and unvalidated) and nothing runs it, and the OSS UI has no management " +
			"page for tests. On DataHub Cloud the server validates the definition when this resource " +
			"is created or updated, and rejects an invalid one with the validation messages. To catch " +
			"an invalid definition at `terraform plan` instead, feed it through the " +
			"`datahub_metadata_test_validation` data source (Cloud only).\n\n" +
			"Requires the `MANAGE_TESTS` privilege (\"Manage Tests\" platform privilege).\n\n" +
			"## Full-aspect ownership\n\n" +
			"This resource owns the whole `testInfo` aspect. `updateTest` rebuilds the aspect from " +
			"the mutation input, so anything set outside Terraform -- including a run schedule or " +
			"status configured in the DataHub Cloud UI -- is reset on the next apply.\n\n" +
			"## URN\n\n" +
			"The URN is `urn:li:test:<test_id>` (deterministic, matching the Python SDK's `TestUrn`). " +
			"When `test_id` is omitted the provider derives `<sanitized-name>-<hash>` client-side, " +
			"because the server would otherwise mint a random UUID. ImportState accepts either the " +
			"full URN or a bare `test_id`, so UUID-suffixed tests created via the DataHub Cloud UI " +
			"can be adopted.\n\n" +
			"## Delete behavior\n\n" +
			"Destroy issues `deleteTest`, a **hard delete** of the test entity. Test results already " +
			"recorded on other entities keep referencing the deleted URN until the next test run " +
			"recomputes them (on OSS, where nothing runs tests, such references persist indefinitely).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Terraform resource id; equal to `test_id`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN (`urn:li:test:<test_id>`).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"test_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Unique id (URN suffix). If omitted, derived from `name` as `<sanitized-name>-<hash>`. Changing it forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-friendly name shown in the DataHub UI.",
			},
			"category": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "User-defined grouping category (e.g. `Governance`, `Data Quality`).",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Human-readable description of what this test asserts.",
			},
			"definition_json": schema.StringAttribute{
				Required:   true,
				CustomType: jsontypes.NormalizedType{},
				MarkdownDescription: "Test definition as a JSON string: an `on` block selecting the entities in scope " +
					"(entity `types` plus optional `conditions`), a `rules` block with the conditions entities must " +
					"pass, and an optional `actions` block. Build it with `jsonencode({...})`. Compared by JSON " +
					"semantic equality, so formatting/key-order differences do not produce spurious plan diffs.",
			},
		},
	}
}

func (r *metadataTestResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var plan metadataTestResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, testID := r.buildInput(&plan, "", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	urn, err := r.client.CreateMetadataTest(ctx, in)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			resp.Diagnostics.AddError("Metadata test already exists",
				"DataHub reports a test at urn:li:test:"+testID+" already exists. "+
					"Import it with `terraform import` to adopt it, or choose a different test_id. "+
					"Underlying error: "+err.Error())
			return
		}
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.TestID = types.StringValue(testID)
	plan.ID = types.StringValue(testID)
	plan.URN = types.StringValue(urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *metadataTestResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var plan, state metadataTestResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// test_id is RequiresReplace, so it is stable across an update; reuse it.
	in, testID := r.buildInput(&plan, state.TestID.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.UpdateMetadataTest(ctx, testID, in); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.TestID = types.StringValue(testID)
	plan.ID = types.StringValue(testID)
	plan.URN = types.StringValue(datahub.MetadataTestURNPrefix + testID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// buildInput assembles the client input from the plan and resolves the
// effective test_id. existingID is empty on Create and the prior test_id on
// Update.
func (r *metadataTestResource) buildInput(plan *metadataTestResourceModel, existingID string, diags *diag.Diagnostics) (datahub.MetadataTestInput, string) {
	name := strings.TrimSpace(plan.Name.ValueString())
	if name == "" {
		diags.AddError("Invalid plan", "name is required")
		return datahub.MetadataTestInput{}, ""
	}
	category := strings.TrimSpace(plan.Category.ValueString())
	if category == "" {
		diags.AddError("Invalid plan", "category is required")
		return datahub.MetadataTestInput{}, ""
	}
	definition := strings.TrimSpace(plan.DefinitionJSON.ValueString())
	if definition == "" {
		diags.AddError("Invalid plan", "definition_json must be a non-empty JSON string")
		return datahub.MetadataTestInput{}, ""
	}

	testID := strings.TrimSpace(existingID)
	if testID == "" && !plan.TestID.IsNull() && !plan.TestID.IsUnknown() {
		testID = strings.TrimSpace(plan.TestID.ValueString())
	}
	if testID == "" {
		testID = uid.DeriveID(name, []byte(name), 48)
	}

	return datahub.MetadataTestInput{
		TestID:         testID,
		Name:           name,
		Category:       category,
		Description:    strVal(plan.Description),
		DefinitionJSON: definition,
	}, testID
}

func (r *metadataTestResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var state metadataTestResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	testID := strings.TrimSpace(state.TestID.ValueString())
	if testID == "" {
		testID = strings.TrimSpace(state.ID.ValueString())
	}
	if testID == "" {
		resp.Diagnostics.AddError("Invalid state", "Missing test_id/id in state; cannot read remote metadata test.")
		return
	}

	info, err := r.client.GetMetadataTestByID(ctx, testID)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if info == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.TestID = types.StringValue(info.ID)
	state.ID = types.StringValue(info.ID)
	state.URN = types.StringValue(datahub.MetadataTestURNPrefix + info.ID)
	if info.Name != "" {
		state.Name = types.StringValue(info.Name)
	}
	if info.Category != "" {
		state.Category = types.StringValue(info.Category)
	}
	state.Description = nullIfEmpty(info.Description)
	if info.DefinitionJSON != "" {
		state.DefinitionJSON = jsontypes.NewNormalizedValue(info.DefinitionJSON)
	} else {
		state.DefinitionJSON = jsontypes.NewNormalizedNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *metadataTestResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}
	var state metadataTestResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	testID := strings.TrimSpace(state.TestID.ValueString())
	if testID == "" {
		testID = strings.TrimSpace(state.ID.ValueString())
	}
	if testID == "" {
		resp.State.RemoveResource(ctx)
		return
	}
	if err := r.client.DeleteMetadataTest(ctx, testID); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *metadataTestResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID",
			"Expected a DataHub metadata test URN (e.g. urn:li:test:my-test) or a bare test_id.")
		return
	}
	testID := strings.TrimPrefix(raw, datahub.MetadataTestURNPrefix)
	if testID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Could not extract a test_id from the provided import ID.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), testID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("test_id"), testID)...)
}
