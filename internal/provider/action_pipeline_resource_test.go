// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

func TestActionPipelineResource_lifecycle_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.ActionPipelineCheckDestroy,
		Steps:                    datahubtesting.ActionPipelineLifecycleSteps(),
	})
}

func TestAcc_ActionPipeline_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireCloud(t) // Cloud-only resource; skips on live OSS targets

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.ActionPipelineCheckDestroy,
		Steps:                    datahubtesting.ActionPipelineLifecycleSteps(),
	})
}

// TestAcc_ActionPipeline_OSS_RejectsWithCloudOnlyError verifies that
// datahub_action_pipeline surfaces a "DataHub Cloud Required" diagnostic when
// applied against an OSS DataHub instance (the entity/mutations are Cloud-only).
func TestAcc_ActionPipeline_OSS_RejectsWithCloudOnlyError(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireOSS(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "datahub_action_pipeline" "oss_error_test" {
  action_id = "tf-example-oss-error"
  name      = "TF Example - OSS Error"
  type      = "dataplex_metadata_sync"
  recipe    = jsonencode({ action = { type = "dataplex_metadata_sync", config = {} } })
}
`,
				ExpectError: regexp.MustCompile(`DataHub Cloud Required`),
			},
		},
	})
}
