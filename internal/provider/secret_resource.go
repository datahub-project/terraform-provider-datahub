// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &secretResource{}
	_ resource.ResourceWithConfigure   = &secretResource{}
	_ resource.ResourceWithImportState = &secretResource{}
)

type secretResource struct {
	client *datahub.Client
}

type secretResourceModel struct {
	ID             types.String `tfsdk:"id"`
	URN            types.String `tfsdk:"urn"`
	Name           types.String `tfsdk:"name"`
	Value          types.String `tfsdk:"value"`
	ValueWOVersion types.Int64  `tfsdk:"value_wo_version"`
	Description    types.String `tfsdk:"description"`
}

func NewSecretResource() resource.Resource {
	return &secretResource{}
}

func (r *secretResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *secretResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (r *secretResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Creates and manages a DataHub Secret.\n\n" +
			"DataHub Secrets are named, encrypted values stored by DataHub and referenced in ingestion recipes " +
			"via `${SECRET_NAME}` placeholders. At run time, the DataHub ingestion executor resolves the " +
			"placeholders by calling the DataHub GraphQL API and substituting the decrypted values into the " +
			"recipe before execution.\n\n" +
			"## Security model\n\n" +
			"The `value` attribute is **WriteOnly**: Terraform sends the plaintext value to the provider on " +
			"each apply, but it is never persisted to `terraform.tfstate`. DataHub encrypts the value " +
			"server-side (AES-GCM-256) before storing it. The plaintext is never returned by any read " +
			"operation.\n\n" +
			"**Requires Terraform CLI 1.11 or later** (WriteOnly attribute support).\n\n" +
			"## Rotation\n\n" +
			"Because `value` is write-only, Terraform cannot detect drift in the value on its own. " +
			"To rotate a secret, update the value in your config and increment `value_wo_version` " +
			"(e.g. from `1` to `2`). Terraform will plan a replacement -- deleting and recreating the " +
			"secret with the new value. The URN (and therefore all recipe references like `${MY_SECRET}`) " +
			"remain unchanged after rotation because the name does not change.\n\n" +
			"```terraform\n" +
			"resource \"datahub_secret\" \"bq_creds\" {\n" +
			"  name             = \"bq-service-account-json\"\n" +
			"  description      = \"Service account for BigQuery ingestion\"\n" +
			"  value            = file(\"${path.module}/bq-key.json\")\n" +
			"  value_wo_version = 1  # bump to rotate\n" +
			"}\n" +
			"```\n\n" +
			"## Post-import note\n\n" +
			"After `terraform import`, the `value` attribute has no recorded state (it was never stored). " +
			"You must set `value` in your config and run `terraform apply` before any subsequent update " +
			"will succeed, because the DataHub update mutation requires the value on every call.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this secret (e.g. `urn:li:dataHubSecret:my-secret`). Use this value as `${my-secret}` in ingestion source recipes.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Unique name for the secret. Becomes the URN suffix and the `${NAME}` reference used in recipes. Changing the name forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"value": schema.StringAttribute{
				Required:            true,
				WriteOnly:           true,
				Sensitive:           true,
				MarkdownDescription: "Plaintext secret value. DataHub encrypts this server-side and never returns it in plaintext. It is not stored in Terraform state. **Requires Terraform CLI 1.11+.**",
			},
			"value_wo_version": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Rotation counter for the `value` attribute. Increment this integer (e.g. `1` -> `2`) to trigger a replacement of the secret with the updated `value` from config. The integer itself is arbitrary; only changes to it matter.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional human-readable description for the secret, shown in the DataHub UI.",
			},
		},
	}
}

func (r *secretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan secretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn, err := r.client.CreateSecret(ctx, datahub.CreateSecretInput{
		Name:        plan.Name.ValueString(),
		Value:       plan.Value.ValueString(),
		Description: plan.Description.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.URN = types.StringValue(urn)
	plan.ID = types.StringValue(urn)
	// value is WriteOnly -- the Framework nullifies it in state automatically.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *secretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state secretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	secret, err := r.client.GetSecretByName(ctx, state.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if secret == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.URN = types.StringValue(secret.URN)
	state.ID = types.StringValue(secret.URN)
	state.Name = types.StringValue(secret.Name)
	if secret.Description != "" {
		state.Description = types.StringValue(secret.Description)
	} else {
		state.Description = types.StringNull()
	}
	// value and value_wo_version are never populated from the server.

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *secretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan secretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state secretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}

	err := r.client.UpdateSecret(ctx, datahub.UpdateSecretInput{
		URN:         urn,
		Name:        plan.Name.ValueString(),
		Value:       plan.Value.ValueString(),
		Description: plan.Description.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.URN = state.URN
	plan.ID = state.ID
	// value is WriteOnly -- the Framework nullifies it in state automatically.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *secretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state secretResourceModel
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

	if err := r.client.DeleteSecret(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *secretResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub secret URN (e.g. urn:li:dataHubSecret:my-secret) or a bare secret name.")
		return
	}

	const urnPrefix = "urn:li:dataHubSecret:"
	var name, urn string
	if strings.HasPrefix(raw, urnPrefix) {
		urn = raw
		name = strings.TrimPrefix(raw, urnPrefix)
	} else {
		name = raw
		urn = urnPrefix + name
	}

	secret, err := r.client.GetSecretByName(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if secret == nil {
		resp.Diagnostics.AddError(
			"Secret not found",
			fmt.Sprintf("No secret named %q was found in DataHub. Verify the name or URN and retry.", name),
		)
		return
	}

	state := secretResourceModel{
		ID:          types.StringValue(urn),
		URN:         types.StringValue(urn),
		Name:        types.StringValue(secret.Name),
		Description: types.StringNull(),
	}
	if secret.Description != "" {
		state.Description = types.StringValue(secret.Description)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
