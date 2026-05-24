// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

func TestAcc_RemoteExecutorPool_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireCloud(t) // Cloud-only resource; skips on live OSS targets
	poolID := tg.Name("tfprovider-pool")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.RemoteExecutorPoolCheckDestroy,
		Steps:                    datahubtesting.RemoteExecutorPoolLifecycleSteps(poolID),
	})
}

func TestAcc_RemoteExecutorPoolDataSource_Read(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireCloud(t) // Cloud-only resource; skips on live OSS targets
	poolID := tg.Name("tfprovider-pool-ds")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.RemoteExecutorPoolDataSourceSteps(poolID),
	})
}

// TestAcc_RemoteExecutorPool_OSS_RejectsWithCloudOnlyError verifies that the
// provider surfaces a clear "DataHub Cloud Required" diagnostic when
// datahub_remote_executor_pool is applied against an OSS DataHub instance.
// Runs only on live OSS targets (testacc-local, testacc-quickstart, or
// testacc-remote without DATAHUB_CLOUD=1). Skipped on mock and Cloud.
func TestAcc_RemoteExecutorPool_OSS_RejectsWithCloudOnlyError(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireOSS(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "datahub_remote_executor_pool" "oss_error_test" {
  pool_id = "oss-error-test-pool"
}
`,
				ExpectError: regexp.MustCompile(`DataHub Cloud Required`),
			},
		},
	})
}
