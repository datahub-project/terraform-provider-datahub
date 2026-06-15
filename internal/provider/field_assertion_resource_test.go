// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

func TestFieldAssertionMetric_lifecycle_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.FieldAssertionCheckDestroy,
		Steps:                    datahubtesting.FieldAssertionMetricLifecycleSteps(),
	})
}

func TestFieldAssertionValues_lifecycle_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.FieldAssertionCheckDestroy,
		Steps:                    datahubtesting.FieldAssertionValuesLifecycleSteps(),
	})
}

func TestFieldAssertion_validation_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.FieldAssertionValidationSteps(),
	})
}

// TestAcc_FieldAssertionMetric_Lifecycle exercises FIELD_METRIC against a live
// Cloud target. FIELD_METRIC reads from a DatasetProfile, so a sqlite dataset
// with a schema suffices (FIELD_VALUES would require a warehouse platform).
func TestAcc_FieldAssertionMetric_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireCloud(t)
	tg.CleanupOrphanedMonitors(t, "urn:li:dataset:(urn:li:dataPlatform:sqlite,tf_assertion_test.tf_test_data,PROD)")
	tg.EnsureDatasetEntityWithSchema(t, "urn:li:dataset:(urn:li:dataPlatform:sqlite,tf_assertion_test.tf_test_data,PROD)")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.FieldAssertionCheckDestroy,
		Steps:                    datahubtesting.FieldAssertionMetricLifecycleSteps(),
	})
}

// TestAcc_FieldAssertion_OSS_RejectsWithCloudOnlyError verifies the Cloud-only
// diagnostic on OSS.
func TestAcc_FieldAssertion_OSS_RejectsWithCloudOnlyError(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireOSS(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "datahub_field_assertion" "oss_error_test" {
  entity_urn           = "urn:li:dataset:(urn:li:dataPlatform:hive,oss.error.table,PROD)"
  field_assertion_type = "FIELD_METRIC"
  field_path           = "id"
  field_type           = "NUMBER"
  metric               = "NULL_COUNT"
  operator             = "EQUAL_TO"
  single_value         = "0"
  source_type          = "DATAHUB_DATASET_PROFILE"
  evaluation_cron      = "0 */8 * * *"
  evaluation_timezone  = "UTC"
  mode                 = "ACTIVE"
}
`,
				ExpectError: regexp.MustCompile(`DataHub Cloud Required`),
			},
		},
	})
}
