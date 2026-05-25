// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
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

// IngestionSourceCheckDestroy is a TestCheckFunc that verifies every
// datahub_ingestion_source in the post-destroy state has been removed from
// DataHub. It constructs a fresh client from DATAHUB_GMS_URL and
// DATAHUB_GMS_TOKEN at call time; for the mock target these are injected via
// t.Setenv before the test runs.
func IngestionSourceCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_ingestion_source" {
			continue
		}
		sourceID := rs.Primary.Attributes["source_id"]
		if sourceID == "" {
			sourceID = rs.Primary.ID
		}
		_, getErr := client.GetIngestionSourceByID(ctx, sourceID)
		if getErr == nil {
			return fmt.Errorf("datahub_ingestion_source %q still exists after destroy", sourceID)
		}
		if errors.Is(getErr, datahub.ErrNotFound) {
			continue
		}
		return fmt.Errorf("CheckDestroy: unexpected error checking datahub_ingestion_source %q: %w", sourceID, getErr)
	}
	return nil
}

// SecretCheckDestroy is a TestCheckFunc that verifies every datahub_secret in
// the post-destroy state has been removed from DataHub. GetSecretByURN returns
// (nil, nil) on 404, so a non-nil secret means the delete did not propagate.
func SecretCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_secret" {
			continue
		}
		urn := rs.Primary.Attributes["urn"]
		if urn == "" {
			urn = rs.Primary.ID
		}
		secret, getErr := client.GetSecretByURN(ctx, urn)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking datahub_secret %q: %w", urn, getErr)
		}
		if secret != nil {
			return fmt.Errorf("datahub_secret %q still exists after destroy", urn)
		}
	}
	return nil
}

// RemoteExecutorPoolLifecycleSteps returns test steps covering create,
// description update, and import for datahub_remote_executor_pool.
//
// poolID must be unique within the target DataHub instance. Mock callers may
// pass a fixed string; live callers should pass a randomized name from
// LiveResourceID.
func RemoteExecutorPoolLifecycleSteps(poolID string) []resource.TestStep {
	const addr = "datahub_remote_executor_pool.test"
	urn := "urn:li:dataHubRemoteExecutorPool:" + poolID

	return []resource.TestStep{
		{
			// Create with description.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_remote_executor_pool" "test" {
  pool_id     = %q
  description = "initial description"
}
`, poolID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("pool_id"), knownvalue.StringExact(poolID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("initial description")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("is_default"), knownvalue.Bool(false)),
			},
		},
		{
			// Update description in-place (pool_id unchanged, no replace).
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_remote_executor_pool" "test" {
  pool_id     = %q
  description = "updated description"
}
`, poolID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("updated description")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
			},
		},
		{
			// Import by URN.
			ResourceName:      addr,
			ImportState:       true,
			ImportStateVerify: true,
		},
	}
}

// RemoteExecutorPoolDataSourceSteps returns test steps that create a pool via
// the resource and then read it back via the data source in the same config.
// This ensures the pool exists for the data source lookup.
func RemoteExecutorPoolDataSourceSteps(poolID string) []resource.TestStep {
	const addr = "data.datahub_remote_executor_pool.test"
	urn := "urn:li:dataHubRemoteExecutorPool:" + poolID

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_remote_executor_pool" "seed" {
  pool_id     = %q
  description = "data source test pool"
}

data "datahub_remote_executor_pool" "test" {
  pool_id    = datahub_remote_executor_pool.seed.pool_id
}
`, poolID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("pool_id"), knownvalue.StringExact(poolID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
			},
		},
	}
}

// RemoteExecutorPoolCheckDestroy verifies every datahub_remote_executor_pool
// in the post-destroy state has been removed from DataHub.
func RemoteExecutorPoolCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_remote_executor_pool" {
			continue
		}
		urn := rs.Primary.Attributes["urn"]
		if urn == "" {
			urn = rs.Primary.ID
		}
		pool, getErr := client.GetRemoteExecutorPoolByURN(ctx, urn)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking datahub_remote_executor_pool %q: %w", urn, getErr)
		}
		if pool != nil {
			return fmt.Errorf("datahub_remote_executor_pool %q still exists after destroy", urn)
		}
	}
	return nil
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
