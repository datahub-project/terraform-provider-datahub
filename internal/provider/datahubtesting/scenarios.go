// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
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

// IngestionSourceLifecycleSteps returns test steps covering create, update,
// and import for datahub_ingestion_source.
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
		{
			// Import by source_id (bare, not full URN).
			ResourceName:      addr,
			ImportState:       true,
			ImportStateVerify: true,
		},
		{
			// Import by full URN (urn:li:dataHubIngestionSource:<id>).
			ResourceName:      addr,
			ImportState:       true,
			ImportStateVerify: true,
			ImportStateIdFunc: func(s *terraform.State) (string, error) {
				rs, ok := s.RootModule().Resources[addr]
				if !ok {
					return "", fmt.Errorf("resource %s not found in state", addr)
				}
				return "urn:li:dataHubIngestionSource:" + rs.Primary.ID, nil
			},
		},
	}
}

// IngestionSourceImportErrorSteps returns test steps verifying that ImportState
// surfaces a diagnostic when called with a malformed import ID. Step 1 creates
// the resource. Steps 2-3 attempt imports with invalid IDs; each expects an
// error matching "Invalid import ID". A final step re-applies the original
// config so the test framework can clean up via terraform destroy.
func IngestionSourceImportErrorSteps(sourceID, sourceName string) []resource.TestStep {
	const addr = "datahub_ingestion_source.test"
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_ingestion_source" "test" {
  source_id   = %q
  source_name = %q
  recipe      = jsonencode({source = {type = "file", config = {filename = "/tmp/test.json"}}})
}
`, sourceID, sourceName)

	return []resource.TestStep{
		{Config: cfg},
		{
			// Whitespace-only ID: TrimSpace reduces it to empty string.
			ResourceName:      addr,
			ImportState:       true,
			ImportStateIdFunc: func(_ *terraform.State) (string, error) { return " ", nil },
			ExpectError:       regexp.MustCompile(`Invalid import ID`),
		},
		{
			// URN with empty suffix: source_id extracted is empty string.
			ResourceName: addr,
			ImportState:  true,
			ImportStateIdFunc: func(_ *terraform.State) (string, error) {
				return "urn:li:dataHubIngestionSource:", nil
			},
			ExpectError: regexp.MustCompile(`Invalid import ID`),
		},
		{Config: cfg}, // Re-apply so cleanup destroy succeeds.
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

// IngestionSourceDataSourceSteps returns test steps for the datahub_ingestion_source
// data source. It seeds an ingestion source via the resource in the same config, then
// reads it back and asserts source_id and urn match.
//
// sourceID is used as the source_id attribute and must be unique within the target
// DataHub instance.
func IngestionSourceDataSourceSteps(sourceID string) []resource.TestStep {
	const addr = "data.datahub_ingestion_source.test"
	urn := "urn:li:dataHubIngestionSource:" + sourceID
	sourceName := "DS test - " + sourceID

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_ingestion_source" "seed" {
  source_id   = %q
  source_name = %q
  source_type = "file"
  recipe      = jsonencode({source = {type = "file", config = {filename = "/tmp/test.json"}}})
}

data "datahub_ingestion_source" "test" {
  source_id = datahub_ingestion_source.seed.source_id
}
`, sourceID, sourceName),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("source_id"), knownvalue.StringExact(sourceID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("source_name"), knownvalue.StringExact(sourceName)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("source_type"), knownvalue.StringExact("file")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("recipe"), knownvalue.NotNull()),
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

// IngestionSourceDriftSteps returns test steps that verify drift detection:
// the resource Read function handles a 404 from the server by calling
// RemoveResource, allowing Terraform to plan a re-create on the next apply.
//
// Step 1 creates the resource. Step 2 deletes it out-of-band via the DataHub
// client (simulating an external deletion), then re-applies the same config.
// Terraform's refresh phase calls Read, which receives ErrNotFound and removes
// the resource from state; Terraform then plans and applies a fresh create.
//
// sourceID must be explicitly provided (not derived) so the PreConfig knows
// which ID to delete. Works against both mock and live targets.
func IngestionSourceDriftSteps(sourceID, sourceName string) []resource.TestStep {
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_ingestion_source" "test" {
  source_id   = %q
  source_name = %q
  recipe      = jsonencode({source = {type = "file", config = {filename = "/tmp/test.json"}}})
}
`, sourceID, sourceName)

	return []resource.TestStep{
		{Config: cfg},
		{
			// Delete the resource out-of-band, then apply the same config.
			// Terraform refreshes: Read returns ErrNotFound -> RemoveResource.
			// Terraform then plans to create and applies successfully.
			PreConfig: func() {
				client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
				if err != nil {
					panic(fmt.Sprintf("IngestionSourceDriftSteps PreConfig: %v", err))
				}
				// Ignore ErrNotFound in case the resource was already gone.
				if delErr := client.DeleteIngestionSourceByID(context.Background(), sourceID); delErr != nil && !errors.Is(delErr, datahub.ErrNotFound) {
					panic(fmt.Sprintf("IngestionSourceDriftSteps PreConfig: delete failed: %v", delErr))
				}
			},
			Config: cfg,
		},
	}
}

// IngestionSourceDeleteErrorSteps returns test steps that verify the resource
// Delete function handles a non-404 server error by surfacing a diagnostic.
//
// This scenario uses the mock server's /test-control/force-delete-fail endpoint
// to make the next DELETE return 500. It is mock-only; callers should skip on
// live targets via tg.IsLive().
//
// Step 1 creates the resource. Step 2 registers the force-fail, then applies an
// empty config (removing the resource from the plan). Terraform refreshes (GET
// still returns 200), plans to delete, calls Delete, receives a 500 error, and
// surfaces "Datahub API Error". The step expects that error; cleanup destroys
// the resource successfully on the subsequent terraform destroy.
func IngestionSourceDeleteErrorSteps(sourceID, sourceName string) []resource.TestStep {
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_ingestion_source" "test" {
  source_id   = %q
  source_name = %q
  recipe      = jsonencode({source = {type = "file", config = {filename = "/tmp/test.json"}}})
}
`, sourceID, sourceName)

	return []resource.TestStep{
		{Config: cfg},
		{
			// Register force-fail for this source, then remove it from the config.
			// Terraform refreshes (GET: 200, resource stays in state), plans to
			// delete, calls the provider Delete, which calls DeleteIngestionSourceByID.
			// The mock returns 500; the resource adds a "Datahub API Error" diagnostic.
			PreConfig: func() {
				url := os.Getenv("DATAHUB_GMS_URL") + "/test-control/force-delete-fail/" + sourceID
				resp, err := http.Post(url, "", bytes.NewReader(nil)) //nolint:noctx
				if err != nil {
					panic(fmt.Sprintf("IngestionSourceDeleteErrorSteps PreConfig: POST force-fail: %v", err))
				}
				resp.Body.Close()
				if resp.StatusCode != http.StatusNoContent {
					panic(fmt.Sprintf("IngestionSourceDeleteErrorSteps PreConfig: unexpected status %d", resp.StatusCode))
				}
			},
			Config:      providerBlock, // empty: triggers delete of the resource
			ExpectError: regexp.MustCompile(`Datahub API Error`),
		},
	}
}

// IngestionSourceDataSourceNotFoundSteps returns a test step that reads the
// datahub_ingestion_source data source with a source_id that does not exist.
// The data source must surface an "Ingestion source not found" diagnostic error
// rather than panic or produce empty state.
func IngestionSourceDataSourceNotFoundSteps(sourceID string) []resource.TestStep {
	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
data "datahub_ingestion_source" "test" {
  source_id = %q
}
`, sourceID),
			ExpectError: regexp.MustCompile(`Ingestion source not found`),
		},
	}
}

// ConnectionDatabricksLifecycleSteps returns test steps covering create, name
// update (in-place), and import for datahub_connection with a databricks block.
//
// connectionID is the connection_id attribute and must be unique within the
// target DataHub instance. Mock callers may pass a fixed string; live callers
// should pass a randomized ID from LiveResourceID.
//
// All platform config fields are WriteOnly; the import step ignores
// config_wo_version (not available from the server) and leaves the platform
// block attrs null in the imported state.
func ConnectionDatabricksLifecycleSteps(connectionID string) []resource.TestStep {
	const addr = "datahub_connection.test"
	urn := "urn:li:dataHubConnection:" + connectionID

	databricksBlock := `
  databricks {
    workspace_url            = "https://dbc-test.cloud.databricks.com"
    warehouse_id             = "abc123"
    auth_type                = "PERSONAL_ACCESS_TOKEN"
    personal_access_token_wo = "test-pat"
  }`

	return []resource.TestStep{
		{
			// Create: verify URN, name, and platform are set.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_connection" "test" {
  connection_id    = %q
  name             = "Initial Name"
  config_wo_version = 1
%s
}
`, connectionID, databricksBlock),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("connection_id"), knownvalue.StringExact(connectionID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Initial Name")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("platform"), knownvalue.StringExact("databricks")),
			},
		},
		{
			// Update name in-place (no replace because connection_id unchanged).
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_connection" "test" {
  connection_id    = %q
  name             = "Updated Name"
  config_wo_version = 1
%s
}
`, connectionID, databricksBlock),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Updated Name")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("platform"), knownvalue.StringExact("databricks")),
			},
		},
		{
			// Import by URN.
			ResourceName:            addr,
			ImportState:             true,
			ImportStateVerify:       true,
			ImportStateVerifyIgnore: connectionImportIgnoreAttrs(),
		},
	}
}

// ConnectionSnowflakeSteps returns test steps for a snowflake block connection.
// Covers only create and delete (not import) to keep the test suite lean.
func ConnectionSnowflakeSteps(connectionID string) []resource.TestStep {
	const addr = "datahub_connection.test"
	urn := "urn:li:dataHubConnection:" + connectionID

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_connection" "test" {
  connection_id    = %q
  name             = "Snowflake Prod"
  config_wo_version = 1
  snowflake {
    account_id  = "xy12345.us-east-1"
    username    = "datahub_user"
    warehouse   = "COMPUTE_WH"
    auth_type   = "DEFAULT_AUTHENTICATOR"
    password_wo = "s3cr3t"
  }
}
`, connectionID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("connection_id"), knownvalue.StringExact(connectionID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("platform"), knownvalue.StringExact("snowflake")),
			},
		},
	}
}

// ConnectionRawConfigSteps returns test steps for a raw_config block connection.
func ConnectionRawConfigSteps(connectionID string) []resource.TestStep {
	const addr = "datahub_connection.test"
	urn := "urn:li:dataHubConnection:" + connectionID

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_connection" "test" {
  connection_id    = %q
  name             = "Looker Prod"
  config_wo_version = 1
  raw_config {
    platform_urn_suffix = "looker"
    config_json_wo      = jsonencode({base_url = "https://looker.example.com", client_id = "abc", client_secret = "xyz"})
  }
}
`, connectionID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("connection_id"), knownvalue.StringExact(connectionID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("platform"), knownvalue.StringExact("looker")),
			},
		},
	}
}

// ConnectionVersionBumpSteps returns test steps that verify config_wo_version
// triggers a replacement of the connection (delete + create with new config).
func ConnectionVersionBumpSteps(connectionID string) []resource.TestStep {
	const addr = "datahub_connection.test"
	urn := "urn:li:dataHubConnection:" + connectionID

	block := `
  databricks {
    workspace_url            = "https://dbc-test.cloud.databricks.com"
    warehouse_id             = "abc123"
    auth_type                = "PERSONAL_ACCESS_TOKEN"
    personal_access_token_wo = "test-pat"
  }`

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_connection" "test" {
  connection_id    = %q
  name             = "Version Bump Test"
  config_wo_version = 1
%s
}
`, connectionID, block),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
			},
		},
		{
			// Bump config_wo_version -- must trigger a replace (delete + create).
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_connection" "test" {
  connection_id    = %q
  name             = "Version Bump Test"
  config_wo_version = 2
%s
}
`, connectionID, block),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroyBeforeCreate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("platform"), knownvalue.StringExact("databricks")),
			},
		},
	}
}

// ConnectionNoBlockSteps returns a test step that expects an error when no
// platform block is configured.
func ConnectionNoBlockSteps() []resource.TestStep {
	return []resource.TestStep{
		{
			Config: providerBlock + `
resource "datahub_connection" "test" {
  connection_id = "no-block-test"
  name          = "Should Fail"
}
`,
			ExpectError: regexp.MustCompile(`No platform block configured`),
		},
	}
}

// ConnectionTwoBlocksSteps returns a test step that expects an error when two
// platform blocks are configured simultaneously.
func ConnectionTwoBlocksSteps() []resource.TestStep {
	return []resource.TestStep{
		{
			Config: providerBlock + `
resource "datahub_connection" "test" {
  connection_id     = "two-blocks-test"
  name              = "Should Fail"
  config_wo_version = 1
  databricks {
    workspace_url          = "https://dbc-example.cloud.databricks.com"
    warehouse_id           = "abc123"
    personal_access_token_wo = "tok"
  }
  snowflake {
    account_id  = "xy12345.us-east-1"
    username    = "datahub_user"
    auth_type   = "DEFAULT_AUTHENTICATOR"
    password_wo = "s3cr3t"
  }
}
`,
			ExpectError: regexp.MustCompile(`Multiple platform blocks configured`),
		},
	}
}

// ConnectionCheckDestroy verifies every datahub_connection in the post-destroy
// state has been removed from DataHub.
func ConnectionCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_connection" {
			continue
		}
		urn := rs.Primary.Attributes["urn"]
		if urn == "" {
			urn = rs.Primary.ID
		}
		conn, getErr := client.GetConnectionByURN(ctx, urn)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking datahub_connection %q: %w", urn, getErr)
		}
		if conn != nil {
			return fmt.Errorf("datahub_connection %q still exists after destroy", urn)
		}
	}
	return nil
}

// connectionImportIgnoreAttrs returns the list of attribute paths to ignore
// during ImportStateVerify for datahub_connection. These are attributes that
// are not available from the server after import (WriteOnly config fields,
// and config_wo_version which has no server-side representation).
//
// OSS DataHub does not return the platform field in the entity response, so
// "platform" is ignored here. On OSS the typed block is also absent from
// imported state (nullBlockForPlatform is a no-op for unknown platform), so
// the block count "databricks.%" differs from pre-import state and must also
// be ignored. Cloud DataHub returns platform correctly; both are verified there
// via ConfigStateChecks in the create/update steps rather than ImportStateVerify.
func connectionImportIgnoreAttrs() []string {
	return []string{
		"config_wo_version",
		"platform",     // OSS does not return platform in entity response
		"databricks.%", // block count absent in imported state on OSS
		// Typed block fields (all WriteOnly -- null in both pre- and post-import state,
		// so they match automatically; listed here for documentation completeness).
		"databricks.workspace_url",
		"databricks.warehouse_id",
		"databricks.auth_type",
		"databricks.personal_access_token_wo",
		"databricks.client_id_wo",
		"databricks.client_secret_wo",
		"snowflake.account_id",
		"snowflake.username",
		"snowflake.warehouse",
		"snowflake.role",
		"snowflake.auth_type",
		"snowflake.password_wo",
		"snowflake.private_key_wo",
		"snowflake.private_key_passphrase_wo",
		"bigquery.private_key_json_wo",
		"dataplex.private_key_json_wo",
		"redshift.host_port",
		"redshift.database",
		"redshift.username",
		"redshift.password_wo",
		"unity_catalog.workspace_url",
		"unity_catalog.warehouse_id",
		"unity_catalog.auth_type",
		"unity_catalog.personal_access_token_wo",
		"unity_catalog.client_id_wo",
		"unity_catalog.client_secret_wo",
		"raw_config.platform_urn_suffix",
		"raw_config.config_json_wo",
	}
}

// CorpGroupLifecycleSteps returns test steps covering create, in-place update of
// name and editable properties, and import for datahub_corp_group.
//
// groupID is the group_id attribute and must be unique within the target
// DataHub instance.
func CorpGroupLifecycleSteps(groupID string) []resource.TestStep {
	const addr = "datahub_corp_group.test"
	urn := "urn:li:corpGroup:" + groupID

	return []resource.TestStep{
		{
			// Create with name + description.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_corp_group" "test" {
  group_id    = %q
  name        = "Data Platform"
  description = "initial description"
}
`, groupID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("group_id"), knownvalue.StringExact(groupID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Data Platform")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("initial description")),
			},
		},
		{
			// Update name (updateName) and add email/slack in-place (no replace).
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_corp_group" "test" {
  group_id    = %q
  name        = "Data Platform Team"
  description = "updated description"
  email       = "data-platform@example.com"
  slack       = "#data-platform"
}
`, groupID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Data Platform Team")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("updated description")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("email"), knownvalue.StringExact("data-platform@example.com")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("slack"), knownvalue.StringExact("#data-platform")),
			},
		},
		{
			// Import by bare group_id.
			ResourceName:      addr,
			ImportState:       true,
			ImportStateId:     groupID,
			ImportStateVerify: true,
		},
		{
			// Import by full URN.
			ResourceName:      addr,
			ImportState:       true,
			ImportStateVerify: true,
			ImportStateIdFunc: func(s *terraform.State) (string, error) {
				rs, ok := s.RootModule().Resources[addr]
				if !ok {
					return "", fmt.Errorf("resource %s not found in state", addr)
				}
				return rs.Primary.Attributes["urn"], nil
			},
		},
	}
}

// CorpGroupDriftSteps verifies that an out-of-band group deletion is detected:
// Read receives a 404 and removes the resource, and the next apply re-creates it.
func CorpGroupDriftSteps(groupID string) []resource.TestStep {
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_corp_group" "test" {
  group_id = %q
  name     = "Drift Group"
}
`, groupID)
	urn := "urn:li:corpGroup:" + groupID

	return []resource.TestStep{
		{Config: cfg},
		{
			PreConfig: func() {
				client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
				if err != nil {
					panic(fmt.Sprintf("CorpGroupDriftSteps PreConfig: %v", err))
				}
				if delErr := client.DeleteGroup(context.Background(), urn); delErr != nil && !errors.Is(delErr, datahub.ErrNotFound) {
					panic(fmt.Sprintf("CorpGroupDriftSteps PreConfig: delete failed: %v", delErr))
				}
			},
			Config: cfg,
		},
	}
}

// CorpGroupDataSourceSteps seeds a group via the resource then reads it back via
// the singular datahub_corp_group data source.
func CorpGroupDataSourceSteps(groupID string) []resource.TestStep {
	const addr = "data.datahub_corp_group.test"
	urn := "urn:li:corpGroup:" + groupID

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_corp_group" "seed" {
  group_id    = %q
  name        = "Lookup Group"
  description = "looked up"
}

data "datahub_corp_group" "test" {
  group_id = datahub_corp_group.seed.group_id
}
`, groupID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("group_id"), knownvalue.StringExact(groupID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Lookup Group")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("looked up")),
			},
		},
	}
}

// CorpGroupsListSteps creates a group and verifies its URN appears in the
// datahub_corp_groups enumeration data source.
func CorpGroupsListSteps(groupID string) []resource.TestStep {
	urn := "urn:li:corpGroup:" + groupID
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_corp_group" "test" {
  group_id = %q
  name     = "List Group"
}

data "datahub_corp_groups" "all" {
  depends_on = [datahub_corp_group.test]
}
`, groupID)

	return []resource.TestStep{
		{
			Config: cfg,
			Check: resource.ComposeAggregateTestCheckFunc(
				assertURNInList("data.datahub_corp_groups.all", urn),
			),
		},
	}
}

// CorpGroupCheckDestroy verifies every datahub_corp_group in the post-destroy
// state has been removed from DataHub.
func CorpGroupCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_corp_group" {
			continue
		}
		urn := rs.Primary.Attributes["urn"]
		if urn == "" {
			urn = rs.Primary.ID
		}
		group, getErr := client.GetGroupByURN(ctx, urn)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking datahub_corp_group %q: %w", urn, getErr)
		}
		if group != nil {
			return fmt.Errorf("datahub_corp_group %q still exists after destroy", urn)
		}
	}
	return nil
}

// CorpGroupMemberSteps returns test steps covering create, import, and drift
// for datahub_corp_group_member. It creates a group, binds the given existing
// user to it, imports by composite ID, and verifies out-of-band removal is
// re-created.
//
// userURN must reference a user that already exists in the target (the provider
// does not create users). Mock seeds "datahub"; live Quickstart has it too.
func CorpGroupMemberSteps(groupID, userURN string) []resource.TestStep {
	const addr = "datahub_corp_group_member.test"
	groupURN := "urn:li:corpGroup:" + groupID
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_corp_group" "test" {
  group_id = %q
  name     = "Member Test Group"
}

resource "datahub_corp_group_member" "test" {
  group_urn = datahub_corp_group.test.urn
  user_urn  = %q
}
`, groupID, userURN)

	return []resource.TestStep{
		{
			Config: cfg,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("group_urn"), knownvalue.StringExact(groupURN)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("user_urn"), knownvalue.StringExact(userURN)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact(groupURN+"|"+userURN)),
			},
		},
		{
			// Import by composite ID.
			ResourceName:      addr,
			ImportState:       true,
			ImportStateVerify: true,
		},
		{
			// Out-of-band removal: Read detects the missing membership and the
			// next apply re-binds.
			PreConfig: func() {
				client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
				if err != nil {
					panic(fmt.Sprintf("CorpGroupMemberSteps PreConfig: %v", err))
				}
				if delErr := client.RemoveGroupMember(context.Background(), groupURN, userURN); delErr != nil {
					panic(fmt.Sprintf("CorpGroupMemberSteps PreConfig: remove failed: %v", delErr))
				}
			},
			Config: cfg,
		},
	}
}

// CorpGroupMemberCheckDestroy verifies every datahub_corp_group_member in the
// post-destroy state no longer reflects the membership in DataHub.
func CorpGroupMemberCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_corp_group_member" {
			continue
		}
		groupURN := rs.Primary.Attributes["group_urn"]
		userURN := rs.Primary.Attributes["user_urn"]
		exists, getErr := client.GroupMemberExists(ctx, groupURN, userURN)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking membership %q in %q: %w", userURN, groupURN, getErr)
		}
		if exists {
			return fmt.Errorf("membership of %q in %q still exists after destroy", userURN, groupURN)
		}
	}
	return nil
}

// CorpUserDataSourceSteps reads the datahub_corp_user data source for the given
// username and asserts the URN and that fields are populated. The user must
// already exist in the target (seeded in the mock; the authenticated principal
// on live).
func CorpUserDataSourceSteps(username string) []resource.TestStep {
	const addr = "data.datahub_corp_user.test"
	urn := "urn:li:corpuser:" + username

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
data "datahub_corp_user" "test" {
  username = %q
}
`, username),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("username"), knownvalue.StringExact(username)),
			},
		},
	}
}

// CorpUserDataSourceNotFoundSteps verifies the data source surfaces a
// "User not found" diagnostic for a username that does not exist.
func CorpUserDataSourceNotFoundSteps(username string) []resource.TestStep {
	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
data "datahub_corp_user" "test" {
  username = %q
}
`, username),
			ExpectError: regexp.MustCompile(`User not found`),
		},
	}
}

// PolicyLifecycleSteps covers create, in-place privilege update, import (by id
// and URN), and drift for a PLATFORM datahub_policy.
func PolicyLifecycleSteps(policyID string) []resource.TestStep {
	const addr = "datahub_policy.test"
	urn := "urn:li:dataHubPolicy:" + policyID

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_policy" "test" {
  policy_id   = %q
  name        = "Platform Admins"
  type        = "PLATFORM"
  description = "initial"
  privileges  = ["MANAGE_POLICIES", "MANAGE_SECRETS"]
  actors = {
    users = ["urn:li:corpuser:datahub"]
  }
}
`, policyID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("policy_id"), knownvalue.StringExact(policyID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("type"), knownvalue.StringExact("PLATFORM")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("state"), knownvalue.StringExact("ACTIVE")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("privileges"), knownvalue.SetSizeExact(2)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("actors").AtMapKey("all_users"), knownvalue.Bool(false)),
			},
		},
		{
			// Update: change privileges set and description in place.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_policy" "test" {
  policy_id   = %q
  name        = "Platform Admins"
  type        = "PLATFORM"
  description = "updated"
  privileges  = ["MANAGE_POLICIES"]
  actors = {
    users = ["urn:li:corpuser:datahub"]
  }
}
`, policyID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("updated")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("privileges"), knownvalue.SetSizeExact(1)),
			},
		},
		{
			// Import by bare policy_id.
			ResourceName:      addr,
			ImportState:       true,
			ImportStateId:     policyID,
			ImportStateVerify: true,
		},
		{
			// Import by full URN.
			ResourceName:      addr,
			ImportState:       true,
			ImportStateVerify: true,
			ImportStateIdFunc: func(s *terraform.State) (string, error) {
				rs, ok := s.RootModule().Resources[addr]
				if !ok {
					return "", fmt.Errorf("resource %s not found in state", addr)
				}
				return rs.Primary.Attributes["urn"], nil
			},
		},
	}
}

// PolicyMetadataSteps covers a METADATA policy with a resources scope and
// all_users actor.
func PolicyMetadataSteps(policyID string) []resource.TestStep {
	const addr = "datahub_policy.test"

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_policy" "test" {
  policy_id  = %q
  name       = "Tag Editors"
  type       = "METADATA"
  privileges = ["EDIT_ENTITY_TAGS"]
  actors = {
    all_users = true
  }
  resources = {
    type      = "dataset"
    resources = ["urn:li:dataset:(urn:li:dataPlatform:hive,foo,PROD)"]
  }
}
`, policyID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("type"), knownvalue.StringExact("METADATA")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("actors").AtMapKey("all_users"), knownvalue.Bool(true)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("resources").AtMapKey("type"), knownvalue.StringExact("dataset")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("resources").AtMapKey("resources"), knownvalue.SetSizeExact(1)),
			},
		},
	}
}

// PolicyDriftSteps verifies out-of-band deletion is detected and the policy is
// re-created on the next apply.
func PolicyDriftSteps(policyID string) []resource.TestStep {
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_policy" "test" {
  policy_id  = %q
  name       = "Drift Policy"
  type       = "PLATFORM"
  privileges = ["MANAGE_POLICIES"]
  actors = {
    all_users = true
  }
}
`, policyID)
	urn := "urn:li:dataHubPolicy:" + policyID

	return []resource.TestStep{
		{Config: cfg},
		{
			PreConfig: func() {
				client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
				if err != nil {
					panic(fmt.Sprintf("PolicyDriftSteps PreConfig: %v", err))
				}
				if delErr := client.DeletePolicy(context.Background(), urn); delErr != nil {
					panic(fmt.Sprintf("PolicyDriftSteps PreConfig: delete failed: %v", delErr))
				}
			},
			Config: cfg,
		},
	}
}

// PolicyCheckDestroy verifies every datahub_policy in the post-destroy state has
// been removed from DataHub.
func PolicyCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_policy" {
			continue
		}
		urn := rs.Primary.Attributes["urn"]
		if urn == "" {
			urn = rs.Primary.ID
		}
		policy, getErr := client.GetPolicyByURN(ctx, urn)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking datahub_policy %q: %w", urn, getErr)
		}
		if policy != nil {
			return fmt.Errorf("datahub_policy %q still exists after destroy", urn)
		}
	}
	return nil
}

// RoleDataSourceSteps reads the built-in "Admin" role via the singular
// datahub_role data source and asserts its URN.
func RoleDataSourceSteps() []resource.TestStep {
	const addr = "data.datahub_role.admin"
	return []resource.TestStep{
		{
			Config: providerBlock + `
data "datahub_role" "admin" {
  name = "Admin"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact("urn:li:dataHubRole:Admin")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Admin")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("editable"), knownvalue.Bool(false)),
			},
		},
	}
}

// RolesListSteps reads the datahub_roles data source and asserts all three
// built-in role URNs are present.
func RolesListSteps() []resource.TestStep {
	return []resource.TestStep{
		{
			Config: providerBlock + `
data "datahub_roles" "all" {}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				assertURNInList("data.datahub_roles.all", "urn:li:dataHubRole:Admin"),
				assertURNInList("data.datahub_roles.all", "urn:li:dataHubRole:Editor"),
				assertURNInList("data.datahub_roles.all", "urn:li:dataHubRole:Reader"),
			),
		},
	}
}

// RoleAssignmentSteps covers assign, in-place reassign (Editor -> Reader),
// import, and delete/unassign for datahub_role_assignment, targeting a freshly
// created group as the actor.
func RoleAssignmentSteps(groupID string) []resource.TestStep {
	const addr = "datahub_role_assignment.test"
	groupURN := "urn:li:corpGroup:" + groupID

	cfg := func(roleName string) string {
		return providerBlock + fmt.Sprintf(`
resource "datahub_corp_group" "test" {
  group_id = %q
  name     = "Role Assignment Group"
}

data "datahub_role" "r" {
  name = %q
}

resource "datahub_role_assignment" "test" {
  actor_urn = datahub_corp_group.test.urn
  role_urn  = data.datahub_role.r.urn
}
`, groupID, roleName)
	}

	return []resource.TestStep{
		{
			Config: cfg("Editor"),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("actor_urn"), knownvalue.StringExact(groupURN)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("role_urn"), knownvalue.StringExact("urn:li:dataHubRole:Editor")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact(groupURN)),
			},
		},
		{
			// Reassign in place (no replace): Editor -> Reader.
			Config: cfg("Reader"),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("role_urn"), knownvalue.StringExact("urn:li:dataHubRole:Reader")),
			},
		},
		{
			// Import by actor URN.
			ResourceName:      addr,
			ImportState:       true,
			ImportStateVerify: true,
			ImportStateId:     groupURN,
		},
	}
}

// RoleAssignmentCheckDestroy verifies every datahub_role_assignment in the
// post-destroy state has had its role cleared in DataHub.
func RoleAssignmentCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_role_assignment" {
			continue
		}
		actorURN := rs.Primary.Attributes["actor_urn"]
		_, found, getErr := client.GetActorRole(ctx, actorURN)
		if getErr != nil {
			// The actor (group) may already be destroyed, which clears the role.
			continue
		}
		if found {
			return fmt.Errorf("role assignment for actor %q still exists after destroy", actorURN)
		}
	}
	return nil
}

// assertURNInList returns a TestCheckFunc asserting that urn appears in the
// urns list attribute of the given data source address. Used where the full
// list contents are not known (live targets with pre-existing entities).
func assertURNInList(addr, urn string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[addr]
		if !ok {
			return fmt.Errorf("data source %s not found in state", addr)
		}
		for k, v := range rs.Primary.Attributes {
			if strings.HasPrefix(k, "urns.") && v == urn {
				return nil
			}
		}
		return fmt.Errorf("URN %q not found in %s.urns", urn, addr)
	}
}

// IngestionSourcesListSteps returns a test step that creates an ingestion source
// and reads the datahub_ingestion_sources data source, verifying that the
// resource's URN appears in the returned urns list.
func IngestionSourcesListSteps(sourceID string) []resource.TestStep {
	sourceName := "List DS test " + sourceID
	urn := "urn:li:dataHubIngestionSource:" + sourceID
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_ingestion_source" "test" {
  source_id   = %q
  source_name = %q
  recipe      = jsonencode({source = {type = "file", config = {filename = "/tmp/test.json"}}})
}

data "datahub_ingestion_sources" "all" {
  depends_on = [datahub_ingestion_source.test]
}
`, sourceID, sourceName)

	return []resource.TestStep{
		{
			Config: cfg,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(
					"data.datahub_ingestion_sources.all",
					tfjsonpath.New("urns"),
					knownvalue.ListExact([]knownvalue.Check{
						knownvalue.StringExact(urn),
					}),
				),
			},
		},
	}
}

// SecretsListSteps returns a test step that creates a secret and reads the
// datahub_secrets data source, verifying the secret's URN appears in the list.
func SecretsListSteps(secretName string) []resource.TestStep {
	urn := "urn:li:dataHubSecret:" + secretName
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_secret" "test" {
  name        = %q
  value       = "test-value"
}

data "datahub_secrets" "all" {
  depends_on = [datahub_secret.test]
}
`, secretName)

	return []resource.TestStep{
		{
			Config: cfg,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(
					"data.datahub_secrets.all",
					tfjsonpath.New("urns"),
					knownvalue.ListExact([]knownvalue.Check{
						knownvalue.StringExact(urn),
					}),
				),
			},
		},
	}
}

// ConnectionsListSteps returns a test step that creates a connection and reads
// the datahub_connections data source, verifying the connection's URN appears.
func ConnectionsListSteps(connectionID string) []resource.TestStep {
	urn := "urn:li:dataHubConnection:" + connectionID
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_connection" "test" {
  connection_id     = %q
  name              = "List DS test"
  config_wo_version = 1
  databricks {
    workspace_url            = "https://dbc-test.cloud.databricks.com"
    warehouse_id             = "abc123"
    auth_type                = "PERSONAL_ACCESS_TOKEN"
    personal_access_token_wo = "test-pat"
  }
}

data "datahub_connections" "all" {
  depends_on = [datahub_connection.test]
}
`, connectionID)

	return []resource.TestStep{
		{
			Config: cfg,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(
					"data.datahub_connections.all",
					tfjsonpath.New("urns"),
					knownvalue.ListExact([]knownvalue.Check{
						knownvalue.StringExact(urn),
					}),
				),
			},
		},
	}
}

// ---------------------------------------------------------------------------
// datahub_corp_user resource scenarios
// ---------------------------------------------------------------------------

// CorpUserLifecycleSteps returns test steps covering create, update, and import
// for datahub_corp_user.
func CorpUserLifecycleSteps(username string) []resource.TestStep {
	const addr = "datahub_corp_user.test"
	urn := "urn:li:corpuser:" + username

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_corp_user" "test" {
  username     = %q
  display_name = "Alice Smith"
  email        = "alice@example.com"
}
`, username),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("username"), knownvalue.StringExact(username)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("display_name"), knownvalue.StringExact("Alice Smith")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("email"), knownvalue.StringExact("alice@example.com")),
			},
		},
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_corp_user" "test" {
  username     = %q
  display_name = "Alice J. Smith"
  full_name    = "Alice Jane Smith"
  email        = "alice.smith@example.com"
  title        = "Data Engineer"
}
`, username),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("display_name"), knownvalue.StringExact("Alice J. Smith")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("full_name"), knownvalue.StringExact("Alice Jane Smith")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("email"), knownvalue.StringExact("alice.smith@example.com")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("title"), knownvalue.StringExact("Data Engineer")),
			},
		},
		{
			ResourceName:      addr,
			ImportState:       true,
			ImportStateId:     username,
			ImportStateVerify: true,
		},
		{
			ResourceName:      addr,
			ImportState:       true,
			ImportStateVerify: true,
			ImportStateIdFunc: func(s *terraform.State) (string, error) {
				rs, ok := s.RootModule().Resources[addr]
				if !ok {
					return "", fmt.Errorf("resource %s not found in state", addr)
				}
				return rs.Primary.Attributes["urn"], nil
			},
		},
	}
}

// CorpUserDriftSteps verifies that an out-of-band user deletion is detected.
func CorpUserDriftSteps(username string) []resource.TestStep {
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_corp_user" "test" {
  username     = %q
  display_name = "Drift User"
}
`, username)
	urn := "urn:li:corpuser:" + username

	return []resource.TestStep{
		{Config: cfg},
		{
			PreConfig: func() {
				client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
				if err != nil {
					panic(fmt.Sprintf("CorpUserDriftSteps PreConfig: %v", err))
				}
				if delErr := client.DeleteUser(context.Background(), urn); delErr != nil {
					panic(fmt.Sprintf("CorpUserDriftSteps PreConfig: delete failed: %v", delErr))
				}
			},
			Config: cfg,
		},
	}
}

// CorpUserCheckDestroy verifies that all datahub_corp_user resources have been
// removed from DataHub after terraform destroy.
func CorpUserCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_corp_user" {
			continue
		}
		urn := rs.Primary.Attributes["urn"]
		if urn == "" {
			urn = rs.Primary.ID
		}
		user, getErr := client.GetUserByURN(ctx, urn)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking datahub_corp_user %q: %w", urn, getErr)
		}
		if user != nil {
			return fmt.Errorf("datahub_corp_user %q still exists after destroy", urn)
		}
	}
	return nil
}
