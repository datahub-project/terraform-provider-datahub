// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

func TestMetadataTestResource_lifecycle_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.MetadataTestCheckDestroy,
		Steps:                    datahubtesting.MetadataTestLifecycleSteps(),
	})
}

func TestAcc_MetadataTest_Lifecycle(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.MetadataTestCheckDestroy,
		Steps:                    datahubtesting.MetadataTestLifecycleSteps(),
	})
}

// TestAcc_MetadataTest_UnknownDefinition feeds definition_json from another
// resource's computed output, so it is unknown during the create plan. A
// literal config can never produce unknown, which is how this bug class
// shipped before: the whole suite passes while every module consumer breaks.
func TestAcc_MetadataTest_UnknownDefinition(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.MetadataTestUnknownDefinitionSteps(),
	})
}

// TestAcc_MetadataTest_InvalidDefinitionRejected verifies that a definition
// the server's test engine rejects fails the apply with the server's
// validation messages. The mock simulates DataHub Cloud, whose createTest
// validates the definition before writing; OSS accepts anything, so this
// scenario is mock/Cloud-only.
func TestAcc_MetadataTest_InvalidDefinitionRejected(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireCloud(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "datahub_metadata_test" "invalid" {
  test_id         = "tf-example-invalid-definition"
  name            = "TF Example - Invalid Definition"
  category        = "Governance"
  definition_json = jsonencode({ nothing = "here" })
}
`,
				ExpectError: regexp.MustCompile(`Failed to validate test definition`),
			},
		},
	})
}

func TestMetadataTestsDataSource_list_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.MetadataTestCheckDestroy,
		Steps:                    datahubtesting.MetadataTestsListSteps("tf-example-list-test"),
	})
}

func TestAcc_MetadataTestsDataSource_List(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("list data source test uses exact-match knownvalue check; live targets may have extra pre-existing resources")
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.MetadataTestCheckDestroy,
		Steps:                    datahubtesting.MetadataTestsListSteps("tf-example-list-test"),
	})
}

func TestMetadataTestValidationDataSource_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.MetadataTestCheckDestroy,
		Steps:                    datahubtesting.MetadataTestValidationDataSourceSteps(),
	})
}

func TestAcc_MetadataTestValidationDataSource(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireCloud(t) // Cloud-only data source; skips on live OSS targets

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.MetadataTestCheckDestroy,
		Steps:                    datahubtesting.MetadataTestValidationDataSourceSteps(),
	})
}

// TestMetadataTestValidationDataSource_invalid_mock verifies that a
// syntactically-valid JSON document the server's engine rejects fails the
// plan with the server's messages surfaced in the diagnostic.
func TestMetadataTestValidationDataSource_invalid_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

data "datahub_metadata_test_validation" "invalid" {
  definition_json = jsonencode({ nothing = "here" })
}
`,
				ExpectError: regexp.MustCompile(`Invalid metadata test definition`),
			},
		},
	})
}
