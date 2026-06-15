// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

func TestSchemaAssertionResource_lifecycle_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.SchemaAssertionCheckDestroy,
		Steps:                    datahubtesting.SchemaAssertionLifecycleSteps(),
	})
}

func TestAcc_SchemaAssertion_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireCloud(t) // Cloud-only resource; skips on live OSS targets
	tg.CleanupOrphanedMonitors(t, "urn:li:dataset:(urn:li:dataPlatform:sqlite,tf_assertion_test.tf_test_data,PROD)")
	tg.EnsureDatasetEntityWithSchema(t, "urn:li:dataset:(urn:li:dataPlatform:sqlite,tf_assertion_test.tf_test_data,PROD)")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.SchemaAssertionCheckDestroy,
		Steps:                    datahubtesting.SchemaAssertionLifecycleSteps(),
	})
}

// TestAcc_SchemaAssertion_OSS_RejectsWithCloudOnlyError verifies that
// datahub_schema_assertion surfaces a "DataHub Cloud Required" diagnostic on OSS.
func TestAcc_SchemaAssertion_OSS_RejectsWithCloudOnlyError(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireOSS(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "datahub_schema_assertion" "oss_error_test" {
  entity_urn          = "urn:li:dataset:(urn:li:dataPlatform:hive,oss.error.table,PROD)"
  compatibility       = "SUPERSET"
  fields              = [{ path = "id", type = "NUMBER" }]
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
