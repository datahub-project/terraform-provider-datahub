// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_Domain_Lifecycle exercises create, in-place update of name and
// description, and import (by id and by URN) for datahub_domain.
func TestAcc_Domain_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	domainID := tg.Name("tfprovider-domain")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.DomainCheckDestroy,
		Steps:                    datahubtesting.DomainLifecycleSteps(domainID),
	})
}

// TestAcc_Domain_CustomPropertiesValidation asserts that invalid
// custom_properties inputs (empty map, empty key, null value, empty value) are
// rejected at plan time by the schema validator.
func TestAcc_Domain_CustomPropertiesValidation(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.DomainCustomPropertiesValidationSteps(),
	})
}

// TestAcc_Domain_ParentChild exercises parent-child creation and in-place
// reparenting via moveDomain for datahub_domain.
func TestAcc_Domain_ParentChild(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	parentID := tg.Name("tfprovider-domain-parent")
	childID := tg.Name("tfprovider-domain-child")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.DomainCheckDestroy,
		Steps:                    datahubtesting.DomainParentChildSteps(parentID, childID),
	})
}

// TestAcc_Domain_DataSource reads a created domain back via the singular
// datahub_domain data source.
func TestAcc_Domain_DataSource(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	domainID := tg.Name("tfprovider-domain-ds")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.DomainCheckDestroy,
		Steps:                    datahubtesting.DomainDataSourceSteps(domainID),
	})
}

// TestAcc_Domains_List verifies the created domain's URN appears in the
// datahub_domains enumeration data source.
func TestAcc_Domains_List(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("list data source is OpenSearch-backed and eventually consistent; a just-created domain may not be indexed at read time")
	}
	domainID := tg.Name("tfprovider-domain-list")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.DomainCheckDestroy,
		Steps:                    datahubtesting.DomainListSteps(domainID),
	})
}
