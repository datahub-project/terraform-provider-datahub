// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_CorpGroup_Lifecycle exercises create, in-place update of name and
// editable properties, and import (by id and by URN) for datahub_corp_group.
func TestAcc_CorpGroup_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	groupID := tg.Name("tfprovider-group")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.CorpGroupCheckDestroy,
		Steps:                    datahubtesting.CorpGroupLifecycleSteps(groupID),
	})
}

// TestAcc_CorpGroup_Drift verifies that an out-of-band deletion is detected and
// the group is re-created on the next apply.
func TestAcc_CorpGroup_Drift(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	groupID := tg.Name("tfprovider-group-drift")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.CorpGroupCheckDestroy,
		Steps:                    datahubtesting.CorpGroupDriftSteps(groupID),
	})
}

// TestAcc_CorpGroup_DataSource reads a created group back via the singular
// datahub_corp_group data source.
func TestAcc_CorpGroup_DataSource(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	groupID := tg.Name("tfprovider-group-ds")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.CorpGroupCheckDestroy,
		Steps:                    datahubtesting.CorpGroupDataSourceSteps(groupID),
	})
}

// TestAcc_CorpGroups_List verifies the created group's URN appears in the
// datahub_corp_groups enumeration data source.
func TestAcc_CorpGroups_List(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("list data source is OpenSearch-backed and eventually consistent; a just-created group may not be indexed at read time")
	}
	groupID := tg.Name("tfprovider-group-list")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.CorpGroupCheckDestroy,
		Steps:                    datahubtesting.CorpGroupsListSteps(groupID),
	})
}
