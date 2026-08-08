// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_PageModule_Lifecycle covers a parameterless module, then the same
// module renamed and given rich-text params, asserting the params round-trip
// and that a parameterless type reads back with params null rather than as an
// empty object.
func TestAcc_PageModule_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	moduleID := tg.Name("tfprovider-page-module")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.PageModuleLifecycleSteps(moduleID),
	})
}

// TestAcc_PageModule_RejectsPersonalScope asserts the provider refuses
// PERSONAL scope at plan time with an explanation, rather than writing a module
// owned by whichever account the provider's token authenticates as.
func TestAcc_PageModule_RejectsPersonalScope(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	moduleID := tg.Name("tfprovider-page-module-personal")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.PageModuleRejectsPersonalScopeSteps(moduleID),
	})
}

// TestAcc_PageTemplate_Lifecycle builds a two-row template over three modules,
// then drops a row and reverses the survivors. The second step is the one that
// matters: it proves the resource owns the whole layout, since a merge would
// leave the dropped row and the original order in place.
func TestAcc_PageTemplate_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	prefix := tg.Name("tfprovider-page-tpl")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.PageTemplateLifecycleSteps(prefix),
	})
}

// TestAcc_PageTemplate_Adoption covers adopting a template that already exists
// and capturing its prior layout, which is what a later destroy restores.
//
// This is the home_default_1 case in miniature: the organisation's home page is
// always adopted rather than created, so the capture path is the one real users
// exercise first.
func TestAcc_PageTemplate_Adoption(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	prefix := tg.Name("tfprovider-page-adopt")

	// The adopted template survives teardown by design -- destroy restores it
	// rather than deleting it -- so the test removes its own subject.
	t.Cleanup(func() {
		if err := datahubtesting.DeleteSeededPageTemplate(prefix + "-adopt"); err != nil {
			t.Logf("could not clean up the seeded template: %v", err)
		}
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.PageTemplateAdoptionSteps(prefix),
	})
}

// TestAcc_PageTemplate_Import imports a template that exists outside Terraform
// and checks the adoption capture happened.
//
// Import is how the docs tell users to adopt home_default_1, so an import that
// skipped the capture would make `import` then `destroy` delete the
// organisation's home page.
func TestAcc_PageTemplate_Import(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	prefix := tg.Name("tfprovider-page-import")

	t.Cleanup(func() {
		if err := datahubtesting.DeleteSeededPageTemplate(prefix + "-import"); err != nil {
			t.Logf("could not clean up the seeded template: %v", err)
		}
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.PageTemplateImportSteps(prefix),
	})
}

// TestAcc_PageTemplate_RowsFromVariable is the mandatory non-literal case for a
// resource with a nested attribute. A literal rows block resolves every schema
// default during config parsing and so never yields an unknown value, which
// means every other test here can pass while each module or for_each use of the
// resource fails before the provider is called. Two earlier resources in this
// provider shipped exactly that bug.
func TestAcc_PageTemplate_RowsFromVariable(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	prefix := tg.Name("tfprovider-page-tpl-var")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.PageTemplateRowsFromVariableSteps(prefix),
	})
}

// TestAcc_HomePageSettings_DataSource reads the pointer that says which
// template the instance renders as everyone's home page. That value is what a
// configuration should feed to datahub_page_template rather than hardcoding the
// bootstrap id.
func TestAcc_HomePageSettings_DataSource(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.HomePageSettingsDataSourceSteps(),
	})
}

// TestAcc_PageTemplate_EmptyRows covers a template with no rows. rows is
// Required and non-null on the wire, so the provider sends an empty array
// rather than omitting the field; this asserts the value round-trips as an
// empty list rather than as null, which would otherwise show a permanent diff.
func TestAcc_PageTemplate_EmptyRows(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	prefix := tg.Name("tfprovider-page-tpl-empty")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.PageTemplateEmptyRowsSteps(prefix),
	})
}
