// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

func TestAcc_StructuredProperty_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	propertyID := tg.Name("tfprovider-sp")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.StructuredPropertyCheckDestroy,
		Steps:                    datahubtesting.StructuredPropertyLifecycleSteps(propertyID),
	})
}

func TestAcc_StructuredProperty_DataSource(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	propertyID := tg.Name("tfprovider-sp-ds")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.StructuredPropertyCheckDestroy,
		Steps:                    datahubtesting.StructuredPropertyDataSourceSteps(propertyID),
	})
}

func TestAcc_StructuredProperty_List(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("list data source is OpenSearch-backed and eventually consistent; a just-created property may not be indexed at read time")
	}
	propertyID := tg.Name("tfprovider-sp-list")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.StructuredPropertyCheckDestroy,
		Steps:                    datahubtesting.StructuredPropertyListSteps(propertyID),
	})
}
