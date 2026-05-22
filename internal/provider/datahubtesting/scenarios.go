// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// providerBlock is the minimal provider configuration block used in all test
// scenarios. The provider reads its endpoint and token from environment variables
// (DATAHUB_GMS_URL and DATAHUB_GMS_TOKEN), which test callers inject via
// t.Setenv before running steps (mock) or export in the shell (live).
const providerBlock = `
provider "datahub" {}
`

// IngestionSourceLifecycleSteps returns test steps covering create and update
// for datahub_ingestion_source. It does not include ImportState because that
// resource does not yet implement ResourceWithImportState.
//
// sourceName is used as the source_name attribute and must be unique within
// the target DataHub instance. Mock callers may pass a fixed string; live
// callers should pass a randomized name from LiveResourceID.
//
// These steps are target-agnostic: they run unchanged against the mock server
// (from *_mock_test.go) or a real DataHub instance (from *_live_test.go).
func IngestionSourceLifecycleSteps(sourceName string) []resource.TestStep {
	const addr = "datahub_ingestion_source.test"
	recipe1 := `jsonencode({source={type="file",config={filename="/tmp/test.json"}}})`
	recipe2 := `jsonencode({source={type="file",config={filename="/tmp/updated.json"}}})`

	return []resource.TestStep{
		{
			// Create: verify computed fields are populated.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_ingestion_source" "test" {
  source_name = %q
  recipe      = %s
}
`, sourceName, recipe1),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("source_name"), knownvalue.StringExact(sourceName)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("source_type"), knownvalue.StringExact("file")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("source_id"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.NotNull()),
			},
		},
		{
			// Update: change recipe, same source_name (no replace).
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_ingestion_source" "test" {
  source_name = %q
  recipe      = %s
}
`, sourceName, recipe2),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("source_name"), knownvalue.StringExact(sourceName)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("source_type"), knownvalue.StringExact("file")),
			},
		},
	}
}

// SecretLifecycleSteps returns test steps covering create, update (description
// change), and import for datahub_secret.
//
// name is used as the secret name and must be unique within the target
// DataHub instance. Mock callers may pass a fixed string; live callers
// should pass a randomized name from LiveResourceID.
//
// The secret value attribute is WriteOnly and requires Terraform CLI 1.11+;
// callers should gate with tfversion.SkipBelow(tfversion.Version1_11_0).
func SecretLifecycleSteps(name string) []resource.TestStep {
	const addr = "datahub_secret.test"
	urn := "urn:li:dataHubSecret:" + name

	return []resource.TestStep{
		{
			// Create: verify URN and name are set.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_secret" "test" {
  name        = %q
  value       = "supersecret"
  description = "initial description"
}
`, name),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact(name)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("initial description")),
			},
		},
		{
			// Update description in-place (no replace because name is unchanged).
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_secret" "test" {
  name        = %q
  value       = "supersecret"
  description = "updated description"
}
`, name),
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

// MeDataSourceStepsExact returns test steps that read the datahub_me data
// source and verify the identity fields match the supplied expected values.
// Use this in mock tests where the served identity is controlled.
func MeDataSourceStepsExact(urn, username, displayName, email string) []resource.TestStep {
	const addr = "data.datahub_me.test"

	return []resource.TestStep{
		{
			Config: providerBlock + `
data "datahub_me" "test" {}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("username"), knownvalue.StringExact(username)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("type"), knownvalue.StringExact("CORP_USER")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("display_name"), knownvalue.StringExact(displayName)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("email"), knownvalue.StringExact(email)),
			},
		},
	}
}

// MeDataSourceStepsAny returns test steps that read the datahub_me data
// source and verify the identity fields are populated (non-null) without
// asserting specific values. Use this in live tests where the identity is
// determined by the PAT owner.
func MeDataSourceStepsAny() []resource.TestStep {
	const addr = "data.datahub_me.test"

	return []resource.TestStep{
		{
			Config: providerBlock + `
data "datahub_me" "test" {}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("username"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("type"), knownvalue.NotNull()),
			},
		},
	}
}
