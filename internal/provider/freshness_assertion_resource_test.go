// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

func TestFreshnessAssertionResource_lifecycle_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.FreshnessAssertionCheckDestroy,
		Steps:                    datahubtesting.FreshnessAssertionLifecycleSteps(),
	})
}

func TestFreshnessAssertionSinceLastCheck_lifecycle_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.FreshnessAssertionCheckDestroy,
		Steps:                    datahubtesting.FreshnessAssertionSinceLastCheckLifecycleSteps(),
	})
}

func TestFreshnessAssertionSchedule_validation_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.FreshnessAssertionScheduleValidationSteps(),
	})
}

func TestAcc_FreshnessAssertionSinceLastCheck_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireCloud(t) // Cloud-only resource; skips on live OSS targets
	tg.CleanupOrphanedMonitors(t, "urn:li:dataset:(urn:li:dataPlatform:sqlite,tf_assertion_test.tf_test_data,PROD)")
	tg.EnsureDatasetEntity(t, "urn:li:dataset:(urn:li:dataPlatform:sqlite,tf_assertion_test.tf_test_data,PROD)")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.FreshnessAssertionCheckDestroy,
		Steps:                    datahubtesting.FreshnessAssertionSinceLastCheckLifecycleSteps(),
	})
}

func TestAcc_FreshnessAssertion_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireCloud(t) // Cloud-only resource; skips on live OSS targets
	tg.CleanupOrphanedMonitors(t, "urn:li:dataset:(urn:li:dataPlatform:hive,freshness.table,PROD)")
	tg.EnsureDatasetEntity(t, "urn:li:dataset:(urn:li:dataPlatform:hive,freshness.table,PROD)")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.FreshnessAssertionCheckDestroy,
		Steps:                    datahubtesting.FreshnessAssertionLifecycleSteps(),
	})
}

// TestAcc_FreshnessAssertion_OSS_RejectsWithCloudOnlyError verifies that
// datahub_freshness_assertion surfaces a "DataHub Cloud Required" diagnostic
// when applied against an OSS DataHub instance.
func TestAcc_FreshnessAssertion_OSS_RejectsWithCloudOnlyError(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireOSS(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "datahub_freshness_assertion" "oss_error_test" {
  entity_urn              = "urn:li:dataset:(urn:li:dataPlatform:hive,oss.error.table,PROD)"
  schedule_type           = "FIXED_INTERVAL"
  fixed_interval_unit     = "HOUR"
  fixed_interval_multiple = 24
  evaluation_cron         = "0 */8 * * *"
  evaluation_timezone     = "UTC"
  source_type             = "AUDIT_LOG"
  mode                    = "ACTIVE"
}
`,
				ExpectError: regexp.MustCompile(`DataHub Cloud Required`),
			},
		},
	})
}
