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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
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
	OriginalRows   types.List   `tfsdk:"original_rows"`
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
			"Creating a new `GLOBAL` template does not make it the page users see, and there is " +
			"no API to repoint DataHub at a different one. Instead, **manage the template that " +
			"is already the default.** Every DataHub instance ships one, seeded as " +
			"`urn:li:dataHubPageTemplate:home_default_1`, and editing it in place is exactly what " +
			"the DataHub UI does. Set `page_template_id = \"home_default_1\"` and this resource " +
			"overwrites that layout.\n\n" +
			"Confirm the id on your instance first, since it is a bootstrap value rather than a " +
			"guarantee: read `homePage.defaultTemplate` from " +
			"`/openapi/v3/entity/globalsettings/urn:li:globalSettings:0`.\n\n" +
			"`terraform destroy` puts back the layout the template had before Terraform " +
			"adopted it, rather than deleting it -- destroying the organisation's home page " +
			"would leave every user without one. The captured layout is visible as " +
			"`original_rows`, and a copy is kept in DataHub so it survives losing your state " +
			"file. A template Terraform *created* is deleted on destroy in the ordinary way.\n\n" +
			"## Does this override what my users see?\n\n" +
			"Only for users who have not customised. DataHub renders a user's own template if " +
			"they have one and falls back to the organisation default otherwise, so applying " +
			"here changes the page for everyone **except** those who have personalised it. On " +
			"DataHub Cloud a user can reset themselves back to the organisation default at any " +
			"time.\n\n" +
			"| Capability | DataHub UI | DataHub Cloud UI | API | This provider |\n" +
			"|---|---|---|---|---|\n" +
			"| Edit the organisation default page | ❌ | ✅ | ✅ | ✅ |\n" +
			"| Edit your own personal page | ❌ | ✅ | ✅ | ❌ |\n" +
			"| Reset yourself to the organisation default | ❌ | ✅ | ✅ | ❌ |\n" +
			"| Point the organisation at a different template | ❌ | ❌ | ❌ | ❌ |\n\n" +
			"Open-source DataHub ships no home-page editing UI at all, so this provider is the " +
			"only practical way to customise the home page there. Personal pages are per-user " +
			"preferences and out of scope for this provider; a `PERSONAL` template written here " +
			"would belong to whichever account the provider's token authenticates as.\n\n" +
			"Managing the organisation default requires the **Manage Home Page Templates** " +
			"privilege.\n\n" +
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
			"original_rows": schema.ListNestedAttribute{
				Computed: true,
				MarkdownDescription: "The layout this template had when Terraform adopted it, " +
					"restored on `terraform destroy`.\n\n" +
					"Populated only when the template already existed at the first apply. It is " +
					"`null` for a template Terraform created, and such a template is deleted on " +
					"destroy in the ordinary way.\n\n" +
					"A copy is also written into DataHub itself, which is the authoritative one " +
					"because it survives the Terraform state being lost. This attribute is the " +
					"fallback, and exists so the layout is visible in `terraform show` rather " +
					"than only inside the provider.",
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"modules": schema.ListAttribute{
							Computed:            true,
							ElementType:         types.StringType,
							MarkdownDescription: "Module URNs in the original row.",
						},
					},
				},
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

// Create adopts an existing template or creates a new one, and the difference
// decides what Delete does later.
//
// A page template frequently already exists when Terraform first writes it --
// managing the organisation's home page means adopting `home_default_1`, which
// DataHub bootstraps on every instance. Deleting such a template on destroy
// would remove something Terraform did not create. So the pre-existing layout
// is captured first, and destroy restores it instead.
//
// Reading before writing is what distinguishes the two cases. Keying this on
// "is it the default template" instead would be wrong: adoption is the property
// that matters, and a catalogue template created by Terraform should destroy in
// the ordinary way whether or not it is the default.
func (r *pageTemplateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan pageTemplateResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := plan.PageTemplateID.ValueString()
	existing, err := r.client.GetPageTemplateByURN(ctx, datahub.PageTemplateURN(id))
	if err != nil {
		resp.Diagnostics.AddError("Unable to check for an existing page template", err.Error())
		return
	}

	if existing == nil {
		plan.OriginalRows = types.ListNull(pageTemplateRowObjectType())
	} else {
		if err := r.client.CaptureTemplateBackup(ctx, id, existing.Rows); err != nil {
			resp.Diagnostics.AddError("Unable to back up the existing page template",
				fmt.Sprintf("%s is already present and Terraform is adopting it, but its current "+
					"layout could not be backed up, so a later destroy could not restore it. "+
					"Refusing to overwrite it: %s", datahub.PageTemplateURN(id), err))
			return
		}
		rows, diags := rowsToList(ctx, existing.Rows)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		plan.OriginalRows = rows
	}

	r.write(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// rowsToList converts client rows into the framework list used by both `rows`
// and `original_rows`.
func rowsToList(ctx context.Context, rows [][]string) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	values := make([]attr.Value, 0, len(rows))
	for _, modules := range rows {
		modList, d := types.ListValueFrom(ctx, types.StringType, modules)
		diags.Append(d...)
		if diags.HasError() {
			return types.ListNull(pageTemplateRowObjectType()), diags
		}
		obj, d2 := types.ObjectValue(pageTemplateRowAttrTypes(), map[string]attr.Value{"modules": modList})
		diags.Append(d2...)
		if diags.HasError() {
			return types.ListNull(pageTemplateRowObjectType()), diags
		}
		values = append(values, obj)
	}
	out, d := types.ListValue(pageTemplateRowObjectType(), values)
	diags.Append(d...)
	return out, diags
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

// Delete restores an adopted template's original layout, or deletes a template
// Terraform created.
//
// Which of the two happens is decided by original_rows, recorded at Create.
// Restoring rather than deleting is what makes `terraform destroy` mean the
// right thing for an adopted platform object: put back what was there before
// Terraform touched it. It also removes the need to refuse the verb outright,
// which an earlier version of this resource did.
//
// When the original layout cannot be found the resource refuses instead of
// guessing. DataHub retains only the last 20 versions of any aspect and a
// managed template writes a version on every change, so the oldest surviving
// version is whatever the window happens to hold -- there is no way to know it
// is the layout Terraform displaced. Restoring an arbitrary point in someone
// else's history would be silent and wrong, so it is reported and left alone.
func (r *pageTemplateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state pageTemplateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urn := state.URN.ValueString()
	id := state.PageTemplateID.ValueString()

	// Terraform created this template, so destroying it is an ordinary delete.
	if state.OriginalRows.IsNull() {
		if err := r.client.DeletePageTemplate(ctx, urn); err != nil {
			resp.Diagnostics.AddError("Unable to delete page template", err.Error())
		}
		return
	}

	// Adopted. Prefer the copy held in DataHub: it survives the Terraform state
	// being lost, which is exactly when the state copy is unavailable.
	restore, source := r.backupRows(ctx, id, &state, &resp.Diagnostics)
	if restore == nil {
		hint := ""
		if oldest := r.client.OldestTemplateRows(ctx, urn); len(oldest) > 0 {
			hint = fmt.Sprintf("\n\nFor reference, the oldest version DataHub still holds for this "+
				"template is %v. That is NOT necessarily the layout Terraform replaced -- only "+
				"the oldest of the last 20 versions -- so the provider will not restore it "+
				"automatically. Read it in full with:\n\n"+
				"    GET /openapi/v3/entity/datahubpagetemplate/%s/datahubpagetemplateproperties?version=1",
				oldest, urn)
		}
		resp.Diagnostics.AddError("Cannot restore the template's original layout",
			fmt.Sprintf("%s was adopted rather than created by Terraform, so destroying it should "+
				"restore the layout it had beforehand. That layout could not be found: the backup "+
				"at %s is missing and original_rows is not in state.\n\n"+
				"Refusing rather than guessing. To stop managing this template and leave it as it "+
				"is:\n\n    terraform state rm datahub_page_template.<name>%s",
				urn, datahub.BackupPageTemplateURN(id), hint))
		return
	}

	if _, err := r.client.UpsertPageTemplate(ctx, datahub.UpsertPageTemplateInput{
		ID:          id,
		Scope:       state.Scope.ValueString(),
		SurfaceType: state.SurfaceType.ValueString(),
		Rows:        restore,
	}); err != nil {
		resp.Diagnostics.AddError("Unable to restore the original page template layout", err.Error())
		return
	}

	if err := r.client.DeletePageTemplate(ctx, datahub.BackupPageTemplateURN(id)); err != nil {
		resp.Diagnostics.AddWarning("Original layout restored, but its backup could not be removed",
			fmt.Sprintf("%s was restored from %s. Delete the backup by hand: %s",
				urn, source, err))
	}
}

// backupRows returns the layout to restore and where it came from, preferring
// the copy stored in DataHub over the copy in Terraform state.
func (r *pageTemplateResource) backupRows(ctx context.Context, id string, state *pageTemplateResourceModel, diags *diag.Diagnostics) ([][]string, string) {
	backupURN := datahub.BackupPageTemplateURN(id)

	// Existence, not emptiness, decides whether a copy is usable. An adopted
	// template that legitimately had no rows captures an empty layout, and
	// restoring it to empty is the correct outcome -- treating empty as "no
	// backup" would refuse the destroy of every template that was empty when
	// Terraform found it. Found by trying to write the test for the refusal
	// branch and discovering it fired on a case that should have restored.
	backup, err := r.client.GetPageTemplateByURN(ctx, backupURN)
	if err != nil {
		diags.AddWarning("Could not read the backup template",
			fmt.Sprintf("Falling back to the copy in Terraform state: %s", err))
	} else if backup != nil {
		return backup.Rows, backupURN
	}

	if state.OriginalRows.IsNull() || state.OriginalRows.IsUnknown() {
		return nil, ""
	}
	rows, d := pageTemplateRowsFromModel(ctx, state.OriginalRows)
	diags.Append(d...)
	if diags.HasError() {
		return nil, ""
	}
	if backup == nil {
		diags.AddWarning("Restored from Terraform state rather than the DataHub backup",
			fmt.Sprintf("The backup at %s was missing, so the original layout came from "+
				"original_rows in state instead.", backupURN))
	}
	return rows, "Terraform state"
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

	// Import is adoption by definition, so capture the layout exactly as Create
	// does. Whatever the template holds right now is the pre-Terraform state:
	// nothing has been applied yet.
	//
	// Skipping this would be the most dangerous gap in the resource. An imported
	// template with no original_rows reads as one Terraform created, so destroy
	// would DELETE it -- and importing home_default_1 is the documented way to
	// adopt the organisation's home page, which would make `import` followed by
	// `destroy` remove the page every user sees.
	//
	// A consequence worth accepting: a template Terraform originally created,
	// then re-imported after losing state, is also treated as adopted, so its
	// destroy restores rather than deletes. Leaving an entity behind is a far
	// smaller surprise than deleting a home page.
	if err := r.client.CaptureTemplateBackup(ctx, tpl.ID, tpl.Rows); err != nil {
		resp.Diagnostics.AddError("Unable to back up the imported page template",
			fmt.Sprintf("%s was read successfully, but its current layout could not be backed "+
				"up, so a later destroy could not restore it: %s", urn, err))
		return
	}
	originals, d := rowsToList(ctx, tpl.Rows)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.OriginalRows = originals

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
