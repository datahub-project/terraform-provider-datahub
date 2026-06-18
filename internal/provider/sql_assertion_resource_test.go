// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

func TestSQLAssertionResource_lifecycle_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.SQLAssertionCheckDestroy,
		Steps:                    datahubtesting.SQLAssertionLifecycleSteps(),
	})
}

func TestSQLAssertionChange_lifecycle_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.SQLAssertionCheckDestroy,
		Steps:                    datahubtesting.SQLAssertionChangeLifecycleSteps(),
	})
}

func TestSQLAssertionChangeType_validation_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.SQLAssertionChangeTypeValidationSteps(),
	})
}

func TestAcc_SQLAssertionChange_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireCloud(t) // Cloud-only resource; skips on live OSS targets
	tg.CleanupOrphanedMonitors(t, "urn:li:dataset:(urn:li:dataPlatform:bigquery,project.dataset.table,PROD)")
	tg.EnsureDatasetEntity(t, "urn:li:dataset:(urn:li:dataPlatform:bigquery,project.dataset.table,PROD)")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.SQLAssertionCheckDestroy,
		Steps:                    datahubtesting.SQLAssertionChangeLifecycleSteps(),
	})
}

func TestAcc_SQLAssertion_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireCloud(t) // Cloud-only resource; skips on live OSS targets
	tg.CleanupOrphanedMonitors(t, "urn:li:dataset:(urn:li:dataPlatform:bigquery,project.dataset.table,PROD)")
	tg.EnsureDatasetEntity(t, "urn:li:dataset:(urn:li:dataPlatform:bigquery,project.dataset.table,PROD)")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.SQLAssertionCheckDestroy,
		Steps:                    datahubtesting.SQLAssertionLifecycleSteps(),
	})
}

// TestAcc_SQLAssertion_OSS_RejectsWithCloudOnlyError verifies that
// datahub_sql_assertion surfaces a "DataHub Cloud Required" diagnostic
// when applied against an OSS DataHub instance.
func TestAcc_SQLAssertion_OSS_RejectsWithCloudOnlyError(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireOSS(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "datahub_sql_assertion" "oss_error_test" {
  entity_urn          = "urn:li:dataset:(urn:li:dataPlatform:hive,oss.error.table,PROD)"
  sql_type            = "METRIC"
  statement           = "SELECT COUNT(*) FROM oss_error_table WHERE value < 0"
  operator            = "EQUAL_TO"
  value               = "0"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  mode                = "ACTIVE"
}
`,
				ExpectError: regexp.MustCompile(`DataHub Cloud Required`),
			},
		},
	})
}
