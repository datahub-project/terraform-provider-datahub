// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

const dataHubPolicyURNPrefix = "urn:li:dataHubPolicy:"

var (
	_ resource.Resource                = &policyResource{}
	_ resource.ResourceWithConfigure   = &policyResource{}
	_ resource.ResourceWithImportState = &policyResource{}
)

type policyResource struct {
	client *datahub.Client
}

type policyResourceModel struct {
	ID          types.String          `tfsdk:"id"`
	URN         types.String          `tfsdk:"urn"`
	PolicyID    types.String          `tfsdk:"policy_id"`
	Name        types.String          `tfsdk:"name"`
	Type        types.String          `tfsdk:"type"`
	State       types.String          `tfsdk:"state"`
	Description types.String          `tfsdk:"description"`
	Privileges  types.Set             `tfsdk:"privileges"`
	Actors      *policyActorsModel    `tfsdk:"actors"`
	Resources   *policyResourcesModel `tfsdk:"resources"`
}

type policyActorsModel struct {
	Users               types.Set  `tfsdk:"users"`
	Groups              types.Set  `tfsdk:"groups"`
	AllUsers            types.Bool `tfsdk:"all_users"`
	AllGroups           types.Bool `tfsdk:"all_groups"`
	ResourceOwners      types.Bool `tfsdk:"resource_owners"`
	ResourceOwnersTypes types.Set  `tfsdk:"resource_owners_types"`
}

type policyResourcesModel struct {
	Type         types.String `tfsdk:"type"`
	Resources    types.Set    `tfsdk:"resources"`
	AllResources types.Bool   `tfsdk:"all_resources"`
}

func NewPolicyResource() resource.Resource {
	return &policyResource{}
}

func (r *policyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	client := pd.Client
	r.client = client
}

func (r *policyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy"
}

func (r *policyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a DataHub access policy (`dataHubPolicy`).\n\n" +
			"Policies grant a set of privileges to a set of actors (users and/or groups), optionally " +
			"scoped to a set of resources. There are two policy types:\n\n" +
			"- `PLATFORM` -- top-level administrative privileges (e.g. `MANAGE_POLICIES`, " +
			"`MANAGE_INGESTION`). Omit the `resources` block.\n" +
			"- `METADATA` -- privileges over metadata entities (e.g. `EDIT_ENTITY_TAGS`), optionally " +
			"scoped via the `resources` block.\n\n" +
			"## Naming\n\n" +
			"`policy_id` becomes the URN suffix (`urn:li:dataHubPolicy:<policy_id>`). Supplying an " +
			"explicit id keeps the URN deterministic and stable, avoiding the random UUID the DataHub " +
			"UI assigns.\n\n" +
			"## List ownership\n\n" +
			"This resource owns the complete `privileges`, `actors`, and `resources` sets and writes " +
			"the full desired state on every apply. Privileges or actors added outside Terraform are " +
			"removed on the next apply. These are modeled as sets, so element order is not significant.\n\n" +
			"## Privileges\n\n" +
			"`privileges` are free-form strings and are not validated by the provider, since the valid " +
			"set differs between DataHub releases and between OSS and DataHub Cloud. See the DataHub " +
			"`PoliciesConfig` for the authoritative list.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN for this policy (e.g., `urn:li:dataHubPolicy:my-policy`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"policy_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Unique identifier for the policy. Becomes the URN suffix (`urn:li:dataHubPolicy:<policy_id>`). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable display name for the policy.",
			},
			"type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Policy type: `PLATFORM` or `METADATA`.",
			},
			"state": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("ACTIVE"),
				MarkdownDescription: "Policy state: `ACTIVE` (enforced) or `INACTIVE`. Defaults to `ACTIVE`.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "Description of the policy's purpose.",
			},
			"privileges": schema.SetAttribute{
				Required:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Set of privilege strings the policy grants (e.g. `[\"MANAGE_POLICIES\"]`). Not validated by the provider.",
			},
			"actors": schema.SingleNestedAttribute{
				Required:            true,
				MarkdownDescription: "The actors the policy's privileges are granted to.",
				Attributes: map[string]schema.Attribute{
					"users": schema.SetAttribute{
						Optional:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "Set of user URNs (e.g. `[\"urn:li:corpuser:alice\"]`).",
					},
					"groups": schema.SetAttribute{
						Optional:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "Set of group URNs (e.g. `[\"urn:li:corpGroup:data-platform\"]`).",
					},
					"all_users": schema.BoolAttribute{
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(false),
						MarkdownDescription: "Apply the policy to all users. Defaults to `false`.",
					},
					"all_groups": schema.BoolAttribute{
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(false),
						MarkdownDescription: "Apply the policy to all groups. Defaults to `false`.",
					},
					"resource_owners": schema.BoolAttribute{
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(false),
						MarkdownDescription: "Apply the policy to owners of the targeted resource (METADATA policies only). Defaults to `false`.",
					},
					"resource_owners_types": schema.SetAttribute{
						Optional:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "Set of ownership-type URNs the resource_owners filter applies to (when `resource_owners = true`).",
					},
				},
			},
			"resources": schema.SingleNestedAttribute{
				Optional:            true,
				MarkdownDescription: "Resource scope for METADATA policies. Omit for platform-wide PLATFORM policies.",
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "The resource type the policy applies to (e.g. `dataset`).",
					},
					"resources": schema.SetAttribute{
						Optional:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "Set of specific resource URNs the policy applies to.",
					},
					"all_resources": schema.BoolAttribute{
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(false),
						MarkdownDescription: "Apply to all resources of `type`. Defaults to `false`.",
					},
				},
			},
		},
	}
}

func (r *policyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan policyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := dataHubPolicyURNPrefix + plan.PolicyID.ValueString()
	in, diags := policyInputFromModel(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := r.client.UpsertPolicy(ctx, urn, in); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = types.StringValue(urn)
	plan.URN = types.StringValue(urn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *policyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state policyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	if urn == "" {
		urn = state.ID.ValueString()
	}

	policy, err := r.client.GetPolicyByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if policy == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	diags := applyPolicyToModel(ctx, policy, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *policyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var plan, state policyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	in, diags := policyInputFromModel(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := r.client.UpsertPolicy(ctx, urn, in); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}

	plan.ID = state.ID
	plan.URN = state.URN
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *policyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured. Ensure provider configuration is set.")
		return
	}

	var state policyResourceModel
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

	if err := r.client.DeletePolicy(ctx, urn); err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *policyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub policy URN (e.g., urn:li:dataHubPolicy:my-policy) or a bare policy ID.")
		return
	}

	var policyID, urn string
	if strings.HasPrefix(raw, dataHubPolicyURNPrefix) {
		urn = raw
		policyID = strings.TrimPrefix(raw, dataHubPolicyURNPrefix)
	} else {
		policyID = raw
		urn = dataHubPolicyURNPrefix + policyID
	}
	if policyID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected a DataHub policy URN (e.g., urn:li:dataHubPolicy:my-policy) or a bare policy ID.")
		return
	}

	policy, err := r.client.GetPolicyByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("DataHub API Error", err.Error())
		return
	}
	if policy == nil {
		resp.Diagnostics.AddError(
			"Policy not found",
			fmt.Sprintf("No policy with URN %q was found in DataHub. Verify the ID or URN and retry.", urn),
		)
		return
	}

	state := policyResourceModel{
		PolicyID: types.StringValue(policy.ID),
	}
	diags := applyPolicyToModel(ctx, policy, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
