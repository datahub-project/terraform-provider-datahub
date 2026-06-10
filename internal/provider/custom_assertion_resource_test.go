// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

func TestCustomAssertionResource_lifecycle_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.CustomAssertionCheckDestroy,
		Steps:                    datahubtesting.CustomAssertionLifecycleSteps(),
	})
}

func TestCustomAssertionDataSource_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.CustomAssertionCheckDestroy,
		Steps:                    datahubtesting.CustomAssertionDataSourceSteps(),
	})
}

func TestAssertionsDataSource_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.CustomAssertionCheckDestroy,
		Steps:                    datahubtesting.CustomAssertionListSteps(),
	})
}

func TestAcc_CustomAssertion_Lifecycle(t *testing.T) {
	datahubtesting.SetupTarget(t) // OSS + Cloud; no RequireCloud

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.CustomAssertionCheckDestroy,
		Steps:                    datahubtesting.CustomAssertionLifecycleSteps(),
	})
}

func TestAcc_CustomAssertion_DataSource(t *testing.T) {
	datahubtesting.SetupTarget(t) // OSS + Cloud; no RequireCloud

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.CustomAssertionCheckDestroy,
		Steps:                    datahubtesting.CustomAssertionDataSourceSteps(),
	})
}

func TestAcc_AssertionsDataSource_List(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("list data source test uses exact-count check; live targets may have pre-existing assertions")
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.CustomAssertionCheckDestroy,
		Steps:                    datahubtesting.CustomAssertionListSteps(),
	})
}
