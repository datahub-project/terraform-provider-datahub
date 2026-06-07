// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_OwnershipType_Lifecycle exercises create (with and without
// description), in-place update, and import (by type_id and by URN) for
// datahub_ownership_type.
func TestAcc_OwnershipType_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	typeID := tg.Name("tfprovider-ownership-type")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.OwnershipTypeCheckDestroy,
		Steps:                    datahubtesting.OwnershipTypeLifecycleSteps(typeID),
	})
}

// TestAcc_OwnershipType_DataSource reads a created ownership type back via the
// singular datahub_ownership_type data source.
func TestAcc_OwnershipType_DataSource(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	typeID := tg.Name("tfprovider-ownership-type-ds")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.OwnershipTypeCheckDestroy,
		Steps:                    datahubtesting.OwnershipTypeDataSourceSteps(typeID),
	})
}

// TestAcc_OwnershipTypes_List verifies that a created ownership type's URN
// appears in the datahub_ownership_types enumeration data source.
func TestAcc_OwnershipTypes_List(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("list data source is GraphQL-backed and eventually consistent; a just-created ownership type may not be indexed at read time")
	}
	typeID := tg.Name("tfprovider-ownership-types-list")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.OwnershipTypeCheckDestroy,
		Steps:                    datahubtesting.OwnershipTypeListSteps(typeID),
	})
}
