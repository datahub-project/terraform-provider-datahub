// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// surfaceTypeValidator restricts surface_type to the three values DataHub
// defines.
//
// Unlike a module's type -- which is passed through unvalidated because the
// catalogue of module types grows every release -- the surface enum names three
// distinct product areas and has been stable. Validating it locally turns a
// typo into a plan-time error instead of an apply-time server rejection.
type surfaceTypeValidator struct{}

func (v surfaceTypeValidator) Description(_ context.Context) string {
	return "surface_type must be HOME_PAGE, ASSET_SUMMARY or CONTEXT_DOCUMENTS"
}

func (v surfaceTypeValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v surfaceTypeValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	switch strings.ToUpper(req.ConfigValue.ValueString()) {
	case "HOME_PAGE", "ASSET_SUMMARY", "CONTEXT_DOCUMENTS":
		return
	default:
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid surface_type",
			fmt.Sprintf("surface_type must be one of HOME_PAGE, ASSET_SUMMARY or CONTEXT_DOCUMENTS, got %q.",
				req.ConfigValue.ValueString()))
	}
}

type pageTemplateResource struct {
	client *datahub.Client
}

// pageTemplateResourceModel holds the template's configuration.
//
// Rows is types.List, not []pageTemplateRowModel, per the nested-attribute rule
// in CLAUDE.md: a Go slice cannot hold an unknown value, so a rows block driven
// from a variable or for_each fails conversion before the provider runs.
//
// Be precise about what that buys *today*, because the comment would otherwise
// claim more than it delivers. The unknown arises from an Optional+Computed
// descendant inside the nested object -- a schema default is what makes an
// attribute plannable-as-unknown -- and this row object has exactly one
// attribute, `modules`, which is Required with no default. So a Go slice would
// currently work, and swapping to one was probed rather than assumed: it broke
// nothing about unknowns.
//
// The framework type stays because it is the shape that survives the schema
// growing. Adding any Optional+Computed attribute to a row -- a per-row width or
// title, say -- makes the hazard live immediately and silently, breaking every
// module and for_each caller while literal-only tests still pass. Choosing the
// safe representation now costs nothing; retrofitting it after a user reports it
// is how this bug shipped twice already.
//
// See the "Unknown Values in Nested Attributes" section of
// docs/design/datahub-model-and-resource-design.md.
type pageTemplateResourceModel struct {
	ID             types.String `tfsdk:"id"`
	URN            types.String `tfsdk:"urn"`
	PageTemplateID types.String `tfsdk:"page_template_id"`
	Scope          types.String `tfsdk:"scope"`
	SurfaceType    types.String `tfsdk:"surface_type"`
	Rows           types.List   `tfsdk:"rows"`
}

type pageTemplateRowModel struct {
	Modules types.List `tfsdk:"modules"`
}

func pageTemplateRowAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"modules": types.ListType{ElemType: types.StringType},
	}
}

func pageTemplateRowObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: pageTemplateRowAttrTypes()}
}

func NewPageTemplateResource() resource.Resource {
	return &pageTemplateResource{}
}

func (r *pageTemplateResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := resourceProviderData(req, resp)
	if pd == nil {
		return
	}
	r.client = pd.Client
}

func (r *pageTemplateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_page_template"
}

func (r *pageTemplateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: ossAndCloudBadge +
			"Creates and manages a DataHub page template -- the ordered layout of " +
			"`datahub_page_module` widgets that makes up a page.\n\n" +
			"## Naming\n\n" +
			"`page_template_id` becomes the URN suffix " +
			"(`urn:li:dataHubPageTemplate:<page_template_id>`) and the provider always sends it. " +
			"DataHub mints a random UUID for templates created without an explicit URN, which is " +
			"what the UI does; supplying it keeps the URN stable across a destroy and re-apply.\n\n" +
			"## This resource owns the whole layout\n\n" +
			"Every apply sends the complete set of rows, because DataHub replaces a template's " +
			"rows with whatever the write carries. A module added to this template outside " +
			"Terraform -- in the DataHub UI, for instance -- is therefore removed on the next " +
			"apply. Manage the full layout here, or do not manage it here at all.\n\n" +
			"## Making it the home page\n\n" +
			"Creating a `GLOBAL` template does not by itself make it the page users see. That " +
			"pointer lives on DataHub's global settings singleton. See " +
			"`datahub_home_page_settings`.\n\n" +
			"## Ordering\n\n" +
			"Reference each module's `urn` rather than writing the URN string by hand. Terraform " +
			"then knows the module must exist before the template that lays it out, and no " +
			"`depends_on` is needed.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Terraform identifier. Same value as `page_template_id`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"urn": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full DataHub URN, `urn:li:dataHubPageTemplate:<page_template_id>`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"page_template_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Identifier forming the URN suffix. Changing this replaces the " +
					"template.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"scope": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("GLOBAL"),
				MarkdownDescription: "Visibility scope. Only `GLOBAL` is supported; `PERSONAL` " +
					"templates are per-user preferences and out of scope for this provider.",
				Validators: []validator.String{personalScopeValidator{}},
			},
			"surface_type": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("HOME_PAGE"),
				MarkdownDescription: "Which DataHub surface this template lays out: `HOME_PAGE`, " +
					"`ASSET_SUMMARY` or `CONTEXT_DOCUMENTS`.",
				Validators:    []validator.String{surfaceTypeValidator{}},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"rows": schema.ListNestedAttribute{
				Required: true,
				MarkdownDescription: "Ordered rows of modules. Row order and the module order " +
					"within each row are both significant and are preserved as written.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"modules": schema.ListAttribute{
							Required:    true,
							ElementType: types.StringType,
							MarkdownDescription: "URNs of the modules in this row, left to right. " +
								"Use `datahub_page_module.<name>.urn` so Terraform orders the " +
								"module before this template.",
						},
					},
				},
			},
		},
	}
}

func (r *pageTemplateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan pageTemplateResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *pageTemplateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan pageTemplateResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// write performs the upsert shared by Create and Update.
func (r *pageTemplateResource) write(ctx context.Context, plan *pageTemplateResourceModel, diags *diag.Diagnostics) {
	rows, d := pageTemplateRowsFromModel(ctx, plan.Rows)
	diags.Append(d...)
	if diags.HasError() {
		return
	}

	in := datahub.UpsertPageTemplateInput{
		ID:   plan.PageTemplateID.ValueString(),
		Rows: rows,
	}
	if !plan.Scope.IsNull() && !plan.Scope.IsUnknown() {
		in.Scope = plan.Scope.ValueString()
	}
	if !plan.SurfaceType.IsNull() && !plan.SurfaceType.IsUnknown() {
		in.SurfaceType = plan.SurfaceType.ValueString()
	}

	urn, err := r.client.UpsertPageTemplate(ctx, in)
	if err != nil {
		diags.AddError("Unable to write page template", err.Error())
		return
	}

	plan.ID = types.StringValue(plan.PageTemplateID.ValueString())
	plan.URN = types.StringValue(urn)
	if plan.Scope.IsUnknown() {
		plan.Scope = types.StringValue("GLOBAL")
	}
	if plan.SurfaceType.IsUnknown() {
		plan.SurfaceType = types.StringValue("HOME_PAGE")
	}
}

func (r *pageTemplateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state pageTemplateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tpl, err := r.client.GetPageTemplateByURN(ctx, state.URN.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read page template", err.Error())
		return
	}
	if tpl == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	diags := applyPageTemplateToModel(ctx, tpl, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *pageTemplateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state pageTemplateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeletePageTemplate(ctx, state.URN.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to delete page template", err.Error())
	}
}

func (r *pageTemplateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := strings.TrimSpace(req.ID)
	if id == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Supply a page template id or its full URN.")
		return
	}

	urn := id
	if !strings.HasPrefix(urn, datahub.PageTemplateURNPrefix) {
		urn = datahub.PageTemplateURN(id)
	}

	tpl, err := r.client.GetPageTemplateByURN(ctx, urn)
	if err != nil {
		resp.Diagnostics.AddError("Unable to import page template", err.Error())
		return
	}
	if tpl == nil {
		resp.Diagnostics.AddError("Page template not found",
			fmt.Sprintf("No page template exists at %s.", urn))
		return
	}

	state := pageTemplateResourceModel{}
	diags := applyPageTemplateToModel(ctx, tpl, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// pageTemplateRowsFromModel converts the rows list into the client shape.
//
// An unknown rows list, or an unknown modules list inside a row, yields no
// rows rather than an error: the value is not resolvable yet, so there is
// nothing to send. Terraform re-plans once it is known.
func pageTemplateRowsFromModel(ctx context.Context, list types.List) ([][]string, diag.Diagnostics) {
	var diags diag.Diagnostics
	if list.IsNull() || list.IsUnknown() {
		return nil, diags
	}

	var rowModels []pageTemplateRowModel
	diags.Append(list.ElementsAs(ctx, &rowModels, false)...)
	if diags.HasError() {
		return nil, diags
	}

	out := make([][]string, 0, len(rowModels))
	for _, rm := range rowModels {
		modules, d := stringList(ctx, rm.Modules)
		diags.Append(d...)
		if diags.HasError() {
			return nil, diags
		}
		if modules == nil {
			modules = []string{}
		}
		out = append(out, modules)
	}
	return out, diags
}

// applyPageTemplateToModel writes a server read back into the Terraform model.
func applyPageTemplateToModel(ctx context.Context, tpl *datahub.PageTemplate, m *pageTemplateResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	m.ID = types.StringValue(tpl.ID)
	m.URN = types.StringValue(tpl.URN)
	m.PageTemplateID = types.StringValue(tpl.ID)
	if tpl.Scope != "" {
		m.Scope = types.StringValue(tpl.Scope)
	}
	if tpl.SurfaceType != "" {
		m.SurfaceType = types.StringValue(tpl.SurfaceType)
	}

	rowValues := make([]attr.Value, 0, len(tpl.Rows))
	for _, modules := range tpl.Rows {
		modList, d := types.ListValueFrom(ctx, types.StringType, modules)
		diags.Append(d...)
		if diags.HasError() {
			return diags
		}
		obj, d2 := types.ObjectValue(pageTemplateRowAttrTypes(), map[string]attr.Value{
			"modules": modList,
		})
		diags.Append(d2...)
		if diags.HasError() {
			return diags
		}
		rowValues = append(rowValues, obj)
	}

	rows, d := types.ListValue(pageTemplateRowObjectType(), rowValues)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	m.Rows = rows
	return diags
}
