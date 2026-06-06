// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_Tag_Lifecycle exercises create (with description and colour),
// in-place rename (via tagProperties aspect write), description and colour
// update, and import (by id and by URN) for datahub_tag.
func TestAcc_Tag_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tagID := tg.Name("tfprovider-tag")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.TagCheckDestroy,
		Steps:                    datahubtesting.TagLifecycleSteps(tagID),
	})
}

// TestAcc_Tag_DataSource reads a created tag back via the singular
// datahub_tag data source.
func TestAcc_Tag_DataSource(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tagID := tg.Name("tfprovider-tag-ds")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.TagCheckDestroy,
		Steps:                    datahubtesting.TagDataSourceSteps(tagID),
	})
}

// TestAcc_Tags_List verifies the created tag's URN appears in the datahub_tags
// enumeration data source.
func TestAcc_Tags_List(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("list data source is OpenSearch-backed and eventually consistent; a just-created tag may not be indexed at read time")
	}
	tagID := tg.Name("tfprovider-tags-list")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.TagCheckDestroy,
		Steps:                    datahubtesting.TagListSteps(tagID),
	})
}
