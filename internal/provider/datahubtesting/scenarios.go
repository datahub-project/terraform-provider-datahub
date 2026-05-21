// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// providerBlock is the minimal provider configuration block used in all test
// scenarios. The provider reads its endpoint and token from environment variables
// (DATAHUB_GMS_URL and DATAHUB_GMS_TOKEN), which test callers inject via
// t.Setenv before running steps.
const providerBlock = `
provider "datahub" {}
`

// IngestionSourceLifecycleSteps returns test steps covering create and update
// for datahub_ingestion_source. It does not include ImportState because that
// resource does not yet implement ResourceWithImportState.
//
// These steps are target-agnostic: they run unchanged against the mock server
// (from *_mock_test.go) or a real DataHub instance (from _live_test.go).
func IngestionSourceLifecycleSteps() []resource.TestStep {
	const addr = "datahub_ingestion_source.test"
	recipe1 := `jsonencode({source={type="file",config={filename="/tmp/test.json"}}})`
	recipe2 := `jsonencode({source={type="file",config={filename="/tmp/updated.json"}}})`

	return []resource.TestStep{
		{
			// Create: verify computed fields are populated.
			Config: providerBlock + `
resource "datahub_ingestion_source" "test" {
  source_name = "Test Source"
  recipe      = ` + recipe1 + `
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("source_name"), knownvalue.StringExact("Test Source")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("source_type"), knownvalue.StringExact("file")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("source_id"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.NotNull()),
			},
		},
		{
			// Update: change recipe, same source_name (no replace).
			Config: providerBlock + `
resource "datahub_ingestion_source" "test" {
  source_name = "Test Source"
  recipe      = ` + recipe2 + `
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("source_name"), knownvalue.StringExact("Test Source")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("source_type"), knownvalue.StringExact("file")),
			},
		},
	}
}

// SecretLifecycleSteps returns test steps covering create, update (description
// change), and import for datahub_secret.
//
// The secret value attribute is WriteOnly and requires Terraform CLI 1.11+;
// callers should gate with tfversion.SkipBelow(tfversion.Version1_11_0).
func SecretLifecycleSteps() []resource.TestStep {
	const addr = "datahub_secret.test"

	return []resource.TestStep{
		{
			// Create: verify URN and name are set.
			Config: providerBlock + `
resource "datahub_secret" "test" {
  name        = "test-secret"
  value       = "supersecret"
  description = "initial description"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("test-secret")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact("urn:li:dataHubSecret:test-secret")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("initial description")),
			},
		},
		{
			// Update description in-place (no replace because name is unchanged).
			Config: providerBlock + `
resource "datahub_secret" "test" {
  name        = "test-secret"
  value       = "supersecret"
  description = "updated description"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("updated description")),
			},
		},
		{
			// Import by URN.
			ResourceName:            addr,
			ImportState:             true,
			ImportStateVerify:       true,
			ImportStateVerifyIgnore: []string{"value", "value_wo_version"},
		},
	}
}

// MeDataSourceSteps returns test steps that read the datahub_me data source
// and verify the identity fields from the mock.
func MeDataSourceSteps() []resource.TestStep {
	const addr = "data.datahub_me.test"

	return []resource.TestStep{
		{
			Config: providerBlock + `
data "datahub_me" "test" {}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact("urn:li:corpuser:testuser")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("username"), knownvalue.StringExact("testuser")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("type"), knownvalue.StringExact("CORP_USER")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("display_name"), knownvalue.StringExact("Test User")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("email"), knownvalue.StringExact("testuser@example.com")),
			},
		},
	}
}
