// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

func TestAcc_IngestionSourcesDataSource_List(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("list data source test uses exact-match knownvalue check; live targets may have extra pre-existing resources")
	}
	sourceID := tg.Name("tfprovider-list-src")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.IngestionSourceCheckDestroy,
		Steps:                    datahubtesting.IngestionSourcesListSteps(sourceID),
	})
}
