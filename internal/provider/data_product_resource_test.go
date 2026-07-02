// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_DataProduct_Lifecycle exercises create (with and without optional
// fields), in-place update, and import (by data_product_id and by URN) for
// datahub_data_product.
func TestAcc_DataProduct_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	dataProductID := tg.Name("tfprovider-data-product")
	domainID := tg.Name("tfprovider-dp-domain")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.DataProductCheckDestroy,
		Steps:                    datahubtesting.DataProductLifecycleSteps(dataProductID, domainID),
	})
}

// TestAcc_DataProduct_CustomPropertiesValidation asserts that invalid
// custom_properties inputs (empty map, empty key, null value, empty value) are
// rejected at plan time by the shared validator.
func TestAcc_DataProduct_CustomPropertiesValidation(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.DataProductCustomPropertiesValidationSteps(),
	})
}

// TestAcc_DataProduct_DataSource reads a created data product back via the
// singular datahub_data_product data source.
func TestAcc_DataProduct_DataSource(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	dataProductID := tg.Name("tfprovider-data-product-ds")
	domainID := tg.Name("tfprovider-dp-domain-ds")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.DataProductCheckDestroy,
		Steps:                    datahubtesting.DataProductDataSourceSteps(dataProductID, domainID),
	})
}

// TestAcc_DataProducts_List verifies that a created data product's URN appears
// in the datahub_data_products enumeration data source.
func TestAcc_DataProducts_List(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("list data source is backed by OpenSearch and is eventually consistent; a just-created data product may not be indexed at read time")
	}
	dataProductID := tg.Name("tfprovider-data-products-list")
	domainID := tg.Name("tfprovider-dp-domain-list")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.DataProductCheckDestroy,
		Steps:                    datahubtesting.DataProductListSteps(dataProductID, domainID),
	})
}
