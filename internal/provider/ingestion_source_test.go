// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

func TestAcc_IngestionSource_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	name := tg.Name("tfprovider-source")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.IngestionSourceCheckDestroy,
		Steps:                    datahubtesting.IngestionSourceLifecycleSteps(name),
	})
}

func TestAcc_IngestionSource_Drift(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	sourceID := tg.Name("tfprovider-drift")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.IngestionSourceCheckDestroy,
		Steps:                    datahubtesting.IngestionSourceDriftSteps(sourceID, "Drift test "+sourceID),
	})
}

func TestAcc_IngestionSource_DeleteError(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("delete-error test requires mock server control endpoint")
	}
	sourceID := tg.Name("tfprovider-delerr")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.IngestionSourceDeleteErrorSteps(sourceID, "Delete error test "+sourceID),
	})
}
