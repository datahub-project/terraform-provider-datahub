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

// ---------------------------------------------------------------------------
// datahub_local_user_login resource scenarios
// ---------------------------------------------------------------------------

// LocalUserLoginWithResetSteps creates a user without initial_password,
// verifies that password_reset_url is populated, and re-applies the same
// config to confirm no spurious drift.
func LocalUserLoginWithResetSteps(username string) []resource.TestStep {
	const addr = "datahub_local_user_login.test"
	email := username + "@example.com"

	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_local_user_login" "test" {
  username  = %q
  full_name = "Reset User"
  email     = %q
}
`, username, email)

	return []resource.TestStep{
		{
			Config: cfg,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("username"), knownvalue.StringExact(username)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("user_urn"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("password_reset_url"), knownvalue.NotNull()),
			},
		},
		{
			Config:             cfg,
			PlanOnly:           true,
			ExpectNonEmptyPlan: false,
		},
	}
}

// LocalUserLoginWithPasswordSteps creates a user with an explicit
// initial_password and verifies that password_reset_url is null.
func LocalUserLoginWithPasswordSteps(username string) []resource.TestStep {
	const addr = "datahub_local_user_login.test"
	email := username + "@example.com"

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_local_user_login" "test" {
  username         = %q
  full_name        = "Password User"
  email            = %q
  initial_password = "test-password-123"
}
`, username, email),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("username"), knownvalue.StringExact(username)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("user_urn"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("password_reset_url"), knownvalue.Null()),
			},
		},
	}
}

// LocalUserLoginImportSteps creates a user via signUp then imports it by
// bare username and by full URN.
func LocalUserLoginImportSteps(username string) []resource.TestStep {
	const addr = "datahub_local_user_login.test"
	email := username + "@example.com"

	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_local_user_login" "test" {
  username  = %q
  full_name = "Import User"
  email     = %q
}
`, username, email)

	return []resource.TestStep{
		{Config: cfg},
		{
			// Import by the actual user_urn from state. On OSS this is
			// urn:li:corpuser:<username>; on Cloud it is
			// urn:li:corpuser:<email> because Cloud derives the URN from
			// the email field. Bare-username import does not work on Cloud.
			// username is ignored in verification because on Cloud the entity
			// stores the email as the username, which differs from the config.
			ResourceName:            addr,
			ImportState:             true,
			ImportStateVerify:       true,
			ImportStateVerifyIgnore: []string{"password_reset_url", "title", "username"},
			ImportStateIdFunc: func(s *terraform.State) (string, error) {
				rs, ok := s.RootModule().Resources[addr]
				if !ok {
					return "", fmt.Errorf("resource %s not found in state", addr)
				}
				return rs.Primary.Attributes["user_urn"], nil
			},
		},
		{
			// Second import pass: same URN, confirms idempotent import.
			ResourceName:            addr,
			ImportState:             true,
			ImportStateVerify:       true,
			ImportStateVerifyIgnore: []string{"password_reset_url", "title", "username"},
			ImportStateIdFunc: func(s *terraform.State) (string, error) {
				rs, ok := s.RootModule().Resources[addr]
				if !ok {
					return "", fmt.Errorf("resource %s not found in state", addr)
				}
				return rs.Primary.Attributes["user_urn"], nil
			},
		},
	}
}

// LocalUserLoginDriftSteps verifies that out-of-band user deletion is detected.
func LocalUserLoginDriftSteps(username string) []resource.TestStep {
	email := username + "@example.com"
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_local_user_login" "test" {
  username  = %q
  full_name = "Drift Login User"
  email     = %q
}
`, username, email)

	return []resource.TestStep{
		{Config: cfg},
		{
			PreConfig: func() {
				client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
				if err != nil {
					panic(fmt.Sprintf("LocalUserLoginDriftSteps PreConfig: %v", err))
				}
				// On Cloud the URN is derived from email; on OSS from username.
				// Try both URNs to cover both platforms.
				urnByUsername := "urn:li:corpuser:" + username
				urnByEmail := "urn:li:corpuser:" + email
				_ = client.DeleteUser(context.Background(), urnByUsername)
				_ = client.DeleteUser(context.Background(), urnByEmail)
			},
			Config: cfg,
		},
	}
}

// LocalUserLoginWithCorpUserSteps tests the two-resource happy path: login
// first (creates entity + credentials), then corp_user upserts profile on top.
func LocalUserLoginWithCorpUserSteps(username string) []resource.TestStep {
	const loginAddr = "datahub_local_user_login.test"
	const userAddr = "datahub_corp_user.test"
	email := username + "@example.com"

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_local_user_login" "test" {
  username  = %q
  full_name = "Two Resource User"
  email     = %q
}

resource "datahub_corp_user" "test" {
  username     = datahub_local_user_login.test.username
  display_name = "Two Resource Display"
  title        = "Staff Engineer"
}
`, username, email),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(loginAddr, tfjsonpath.New("user_urn"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue(userAddr, tfjsonpath.New("urn"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue(userAddr, tfjsonpath.New("display_name"), knownvalue.StringExact("Two Resource Display")),
				statecheck.ExpectKnownValue(userAddr, tfjsonpath.New("title"), knownvalue.StringExact("Staff Engineer")),
			},
		},
	}
}

// LocalUserLoginCloudUpgradeSteps tests the Cloud upgrade path: create a
// catalog-only user first (no credentials), then add credentials via
// local_user_login. This succeeds on Cloud (default mock behavior) because the
// signUp guard only rejects users that already have credentials.
func LocalUserLoginCloudUpgradeSteps(username string) []resource.TestStep {
	const addr = "datahub_local_user_login.test"
	urn := "urn:li:corpuser:" + username

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_corp_user" "seed" {
  username     = %q
  display_name = "Cloud Upgrade User"
  full_name    = "Cloud Upgrade User"
  email        = "cloud-upgrade@example.com"
  title        = "Other"
}
`, username),
		},
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_corp_user" "seed" {
  username     = %q
  display_name = "Cloud Upgrade User"
  full_name    = "Cloud Upgrade User"
  email        = "cloud-upgrade@example.com"
  title        = "Other"
}

resource "datahub_local_user_login" "test" {
  username  = %q
  full_name = "Cloud Upgrade User"
  email     = "cloud-upgrade@example.com"
}
`, username, username),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("user_urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("password_reset_url"), knownvalue.NotNull()),
			},
		},
	}
}

// LocalUserLoginOSSRejectsExistingSteps enables OSS signUp mode on the mock,
// creates a catalog-only user, then verifies that signUp is rejected because
// the entity already exists (regardless of credentials).
func LocalUserLoginOSSRejectsExistingSteps(username string) []resource.TestStep {
	return []resource.TestStep{
		{
			PreConfig: func() {
				setOSSSignUpMode(true)
			},
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_corp_user" "seed" {
  username     = %q
  display_name = "OSS Reject User"
}
`, username),
		},
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_corp_user" "seed" {
  username     = %q
  display_name = "OSS Reject User"
}

resource "datahub_local_user_login" "test" {
  username  = %q
  full_name = "OSS Reject User"
  email     = "oss-reject@example.com"
}
`, username, username),
			ExpectError: regexp.MustCompile(`(?i)already exists`),
		},
	}
}

// LocalUserLoginAlreadyHasCredentialsSteps creates a user via signUp (giving
// them credentials), then attempts a second signUp for the same username.
// Fails on both OSS and Cloud because the user already has credentials.
func LocalUserLoginAlreadyHasCredentialsSteps(username string) []resource.TestStep {
	cfg1 := providerBlock + fmt.Sprintf(`
resource "datahub_local_user_login" "first" {
  username  = %q
  full_name = "Creds User"
  email     = "creds@example.com"
}
`, username)

	return []resource.TestStep{
		{Config: cfg1},
		{
			Config: cfg1 + fmt.Sprintf(`
resource "datahub_local_user_login" "second" {
  username  = %q
  full_name = "Creds User Again"
  email     = "creds-again@example.com"
}
`, username),
			ExpectError: regexp.MustCompile(`(?i)already exists`),
		},
	}
}

// setOSSSignUpMode calls the mock's test-control endpoint to toggle OSS signUp
// behavior.
func setOSSSignUpMode(enable bool) {
	gmsURL := os.Getenv("DATAHUB_GMS_URL")
	method := http.MethodPost
	if !enable {
		method = http.MethodDelete
	}
	req, err := http.NewRequestWithContext(context.Background(), method, gmsURL+"/test-control/oss-signup-mode", nil)
	if err != nil {
		panic(fmt.Sprintf("setOSSSignUpMode: %v", err))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(fmt.Sprintf("setOSSSignUpMode: %v", err))
	}
	resp.Body.Close()
}

// LocalUserLoginCheckDestroy verifies that all datahub_local_user_login
// resources have been removed from DataHub after terraform destroy.
func LocalUserLoginCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_local_user_login" {
			continue
		}
		urn := rs.Primary.Attributes["user_urn"]
		if urn == "" {
			urn = rs.Primary.ID
		}
		user, getErr := client.GetUserByURN(ctx, urn)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking datahub_local_user_login %q: %w", urn, getErr)
		}
		if user != nil {
			return fmt.Errorf("datahub_local_user_login %q still exists after destroy", urn)
		}
	}
	return nil
}

// DomainLifecycleSteps returns test steps covering create, in-place update of
// name and description, and import (by id and by URN) for datahub_domain.
//
// domainID is the domain_id attribute and must be unique within the target
// DataHub instance.
func DomainLifecycleSteps(domainID string) []resource.TestStep {
	const addr = "datahub_domain.test"
	urn := "urn:li:domain:" + domainID

	return []resource.TestStep{
		{
			// Create a root domain with name and description.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_domain" "test" {
  domain_id   = %q
  name        = "Finance"
  description = "Finance domain"
}
`, domainID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("domain_id"), knownvalue.StringExact(domainID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Finance")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("Finance domain")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("parent_domain"), knownvalue.Null()),
			},
		},
		{
			// Rename and update description in place (no replacement).
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_domain" "test" {
  domain_id   = %q
  name        = "Finance & Risk"
  description = "Finance and risk management domain"
}
`, domainID),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Finance & Risk")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("Finance and risk management domain")),
			},
		},
		{
			// Import by bare domain_id.
			ResourceName:      addr,
			ImportState:       true,
			ImportStateId:     domainID,
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

// DomainParentChildSteps returns test steps covering parent-child creation and
// in-place reparenting via moveDomain for datahub_domain.
//
// parentID and childID must be unique within the target DataHub instance.
func DomainParentChildSteps(parentID, childID string) []resource.TestStep {
	const parentAddr = "datahub_domain.parent"
	const childAddr = "datahub_domain.child"
	parentURN := "urn:li:domain:" + parentID
	childURN := "urn:li:domain:" + childID

	cfgWithParent := providerBlock + fmt.Sprintf(`
resource "datahub_domain" "parent" {
  domain_id   = %q
  name        = "Operations"
  description = "Operations area"
}

resource "datahub_domain" "child" {
  domain_id     = %q
  name          = "Clearing"
  description   = "Clearing and settlement"
  parent_domain = datahub_domain.parent.urn
}
`, parentID, childID)

	// depends_on preserves the destroy ordering (child before parent) even after
	// parent_domain is removed -- without it Terraform has no edge in the graph
	// and may destroy parent first, hitting the hard-delete child guard.
	cfgChildAtRoot := providerBlock + fmt.Sprintf(`
resource "datahub_domain" "parent" {
  domain_id   = %q
  name        = "Operations"
  description = "Operations area"
}

resource "datahub_domain" "child" {
  domain_id   = %q
  name        = "Clearing"
  description = "Clearing and settlement"
  depends_on  = [datahub_domain.parent]
}
`, parentID, childID)

	return []resource.TestStep{
		{
			// Create parent and child; child references parent via .urn.
			Config: cfgWithParent,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(parentAddr, tfjsonpath.New("urn"), knownvalue.StringExact(parentURN)),
				statecheck.ExpectKnownValue(childAddr, tfjsonpath.New("urn"), knownvalue.StringExact(childURN)),
				statecheck.ExpectKnownValue(childAddr, tfjsonpath.New("parent_domain"), knownvalue.StringExact(parentURN)),
			},
		},
		{
			// Remove parent_domain from child (reparent to root via moveDomain).
			// Must not trigger replacement.
			Config: cfgChildAtRoot,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(childAddr, plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction(parentAddr, plancheck.ResourceActionNoop),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(childAddr, tfjsonpath.New("parent_domain"), knownvalue.Null()),
			},
		},
	}
}

// DomainDataSourceSteps seeds a domain via the resource then reads it back via
// the singular datahub_domain data source.
func DomainDataSourceSteps(domainID string) []resource.TestStep {
	const addr = "data.datahub_domain.test"
	urn := "urn:li:domain:" + domainID

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_domain" "seed" {
  domain_id   = %q
  name        = "Lookup Domain"
  description = "looked up"
}

data "datahub_domain" "test" {
  domain_id  = datahub_domain.seed.domain_id
}
`, domainID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("domain_id"), knownvalue.StringExact(domainID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Lookup Domain")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("looked up")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("parent_domain"), knownvalue.StringExact("")),
			},
		},
	}
}

// DomainListSteps creates a domain and verifies its URN appears in the
// datahub_domains enumeration data source.
func DomainListSteps(domainID string) []resource.TestStep {
	urn := "urn:li:domain:" + domainID
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_domain" "test" {
  domain_id = %q
  name      = "List Domain"
}

data "datahub_domains" "all" {
  depends_on = [datahub_domain.test]
}
`, domainID)

	return []resource.TestStep{
		{
			Config: cfg,
			Check: resource.ComposeAggregateTestCheckFunc(
				assertURNInList("data.datahub_domains.all", urn),
			),
		},
	}
}

// DomainCheckDestroy verifies every datahub_domain in the post-destroy state
// has been removed from DataHub.
func DomainCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_domain" {
			continue
		}
		urn := rs.Primary.Attributes["urn"]
		if urn == "" {
			urn = rs.Primary.ID
		}
		domain, getErr := client.GetDomainByURN(ctx, urn)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking datahub_domain %q: %w", urn, getErr)
		}
		if domain != nil {
			return fmt.Errorf("datahub_domain %q still exists after destroy", urn)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Glossary node scenarios
// ---------------------------------------------------------------------------

// GlossaryNodeLifecycleSteps returns test steps covering create, in-place
// update of name and description, and import (by id and by URN) for
// datahub_glossary_node.
//
// nodeID is the node_id attribute and must be unique within the target
// DataHub instance.
func GlossaryNodeLifecycleSteps(nodeID string) []resource.TestStep {
	const addr = "datahub_glossary_node.test"
	urn := "urn:li:glossaryNode:" + nodeID

	return []resource.TestStep{
		{
			// Create a root-level term group with name and description.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_glossary_node" "test" {
  node_id     = %q
  name        = "Finance"
  description = "Finance term group"
}
`, nodeID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("node_id"), knownvalue.StringExact(nodeID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Finance")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("Finance term group")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("parent_node"), knownvalue.Null()),
			},
		},
		{
			// Rename and update description in place (no replacement).
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_glossary_node" "test" {
  node_id     = %q
  name        = "Finance & Risk"
  description = "Finance and risk management term group"
}
`, nodeID),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Finance & Risk")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("Finance and risk management term group")),
			},
		},
		{
			// Import by bare node_id.
			ResourceName:      addr,
			ImportState:       true,
			ImportStateId:     nodeID,
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

// GlossaryNodeParentChildSteps returns test steps covering parent-child
// creation and in-place reparenting via updateParentNode for
// datahub_glossary_node.
func GlossaryNodeParentChildSteps(parentID, childID string) []resource.TestStep {
	const parentAddr = "datahub_glossary_node.parent"
	const childAddr = "datahub_glossary_node.child"
	parentURN := "urn:li:glossaryNode:" + parentID
	childURN := "urn:li:glossaryNode:" + childID

	cfgWithParent := providerBlock + fmt.Sprintf(`
resource "datahub_glossary_node" "parent" {
  node_id     = %q
  name        = "Business"
  description = "Business terms"
}

resource "datahub_glossary_node" "child" {
  node_id     = %q
  name        = "Finance"
  description = "Finance terms"
  parent_node = datahub_glossary_node.parent.urn
}
`, parentID, childID)

	cfgChildAtRoot := providerBlock + fmt.Sprintf(`
resource "datahub_glossary_node" "parent" {
  node_id     = %q
  name        = "Business"
  description = "Business terms"
}

resource "datahub_glossary_node" "child" {
  node_id    = %q
  name       = "Finance"
  description = "Finance terms"
  depends_on = [datahub_glossary_node.parent]
}
`, parentID, childID)

	return []resource.TestStep{
		{
			// Create parent and child; child references parent via .urn.
			Config: cfgWithParent,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(parentAddr, tfjsonpath.New("urn"), knownvalue.StringExact(parentURN)),
				statecheck.ExpectKnownValue(childAddr, tfjsonpath.New("urn"), knownvalue.StringExact(childURN)),
				statecheck.ExpectKnownValue(childAddr, tfjsonpath.New("parent_node"), knownvalue.StringExact(parentURN)),
			},
		},
		{
			// Remove parent_node from child (reparent to root).
			// Must not trigger replacement.
			Config: cfgChildAtRoot,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(childAddr, plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction(parentAddr, plancheck.ResourceActionNoop),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(childAddr, tfjsonpath.New("parent_node"), knownvalue.Null()),
			},
		},
	}
}

// GlossaryNodeDataSourceSteps seeds a glossary node then reads it back via
// the singular datahub_glossary_node data source.
func GlossaryNodeDataSourceSteps(nodeID string) []resource.TestStep {
	const addr = "data.datahub_glossary_node.test"
	urn := "urn:li:glossaryNode:" + nodeID

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_glossary_node" "seed" {
  node_id     = %q
  name        = "Lookup Node"
  description = "looked up"
}

data "datahub_glossary_node" "test" {
  node_id = datahub_glossary_node.seed.node_id
}
`, nodeID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("node_id"), knownvalue.StringExact(nodeID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Lookup Node")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("looked up")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("parent_node"), knownvalue.StringExact("")),
			},
		},
	}
}

// GlossaryNodeListSteps creates a glossary node and verifies its URN appears
// in the datahub_glossary_nodes enumeration data source.
func GlossaryNodeListSteps(nodeID string) []resource.TestStep {
	urn := "urn:li:glossaryNode:" + nodeID
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_glossary_node" "test" {
  node_id = %q
  name    = "List Node"
}

data "datahub_glossary_nodes" "all" {
  depends_on = [datahub_glossary_node.test]
}
`, nodeID)

	return []resource.TestStep{
		{
			Config: cfg,
			Check: resource.ComposeAggregateTestCheckFunc(
				assertURNInList("data.datahub_glossary_nodes.all", urn),
			),
		},
	}
}

// GlossaryNodeCheckDestroy verifies every datahub_glossary_node in the
// post-destroy state has been removed from DataHub.
func GlossaryNodeCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_glossary_node" {
			continue
		}
		urn := rs.Primary.Attributes["urn"]
		if urn == "" {
			urn = rs.Primary.ID
		}
		node, getErr := client.GetGlossaryNodeByURN(ctx, urn)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking datahub_glossary_node %q: %w", urn, getErr)
		}
		if node != nil {
			return fmt.Errorf("datahub_glossary_node %q still exists after destroy", urn)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Glossary term scenarios
// ---------------------------------------------------------------------------

// GlossaryTermLifecycleSteps returns test steps covering create, in-place
// update of name and description, and import (by id and by URN) for
// datahub_glossary_term.
//
// nodeID is used to create a parent term group; termID is the term_id
// attribute. Both must be unique within the target DataHub instance.
func GlossaryTermLifecycleSteps(nodeID, termID string) []resource.TestStep {
	const addr = "datahub_glossary_term.test"
	nodeURN := "urn:li:glossaryNode:" + nodeID
	termURN := "urn:li:glossaryTerm:" + termID

	return []resource.TestStep{
		{
			// Create a term group and a term beneath it.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_glossary_node" "parent" {
  node_id = %q
  name    = "Finance"
}

resource "datahub_glossary_term" "test" {
  term_id     = %q
  name        = "Revenue"
  description = "Total revenue"
  parent_node = datahub_glossary_node.parent.urn
}
`, nodeID, termID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("term_id"), knownvalue.StringExact(termID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(termURN)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Revenue")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("Total revenue")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("parent_node"), knownvalue.StringExact(nodeURN)),
			},
		},
		{
			// Rename and update description in place (no replacement).
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_glossary_node" "parent" {
  node_id = %q
  name    = "Finance"
}

resource "datahub_glossary_term" "test" {
  term_id     = %q
  name        = "Gross Revenue"
  description = "Total gross revenue before deductions"
  parent_node = datahub_glossary_node.parent.urn
}
`, nodeID, termID),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Gross Revenue")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("Total gross revenue before deductions")),
			},
		},
		{
			// Import by bare term_id.
			ResourceName:      addr,
			ImportState:       true,
			ImportStateId:     termID,
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

// GlossaryTermReparentSteps creates a node and term, then reparents the term
// to root (removes parent_node) in place via updateParentNode.
func GlossaryTermReparentSteps(nodeID, termID string) []resource.TestStep {
	const nodeAddr = "datahub_glossary_node.group"
	const termAddr = "datahub_glossary_term.term"
	nodeURN := "urn:li:glossaryNode:" + nodeID

	cfgWithParent := providerBlock + fmt.Sprintf(`
resource "datahub_glossary_node" "group" {
  node_id = %q
  name    = "Operations"
}

resource "datahub_glossary_term" "term" {
  term_id     = %q
  name        = "Cost"
  parent_node = datahub_glossary_node.group.urn
}
`, nodeID, termID)

	cfgAtRoot := providerBlock + fmt.Sprintf(`
resource "datahub_glossary_node" "group" {
  node_id = %q
  name    = "Operations"
}

resource "datahub_glossary_term" "term" {
  term_id    = %q
  name       = "Cost"
  depends_on = [datahub_glossary_node.group]
}
`, nodeID, termID)

	return []resource.TestStep{
		{
			Config: cfgWithParent,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(termAddr, tfjsonpath.New("parent_node"), knownvalue.StringExact(nodeURN)),
			},
		},
		{
			// Detach from parent node -- must be an Update, not Replace.
			Config: cfgAtRoot,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(termAddr, plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction(nodeAddr, plancheck.ResourceActionNoop),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(termAddr, tfjsonpath.New("parent_node"), knownvalue.Null()),
			},
		},
	}
}

// GlossaryTermDataSourceSteps seeds a term then reads it back via the singular
// datahub_glossary_term data source.
func GlossaryTermDataSourceSteps(nodeID, termID string) []resource.TestStep {
	const addr = "data.datahub_glossary_term.test"
	termURN := "urn:li:glossaryTerm:" + termID

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_glossary_node" "parent" {
  node_id = %q
  name    = "Finance"
}

resource "datahub_glossary_term" "seed" {
  term_id     = %q
  name        = "Profit"
  description = "net profit"
  parent_node = datahub_glossary_node.parent.urn
}

data "datahub_glossary_term" "test" {
  term_id = datahub_glossary_term.seed.term_id
}
`, nodeID, termID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("term_id"), knownvalue.StringExact(termID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(termURN)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Profit")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("net profit")),
			},
		},
	}
}

// GlossaryTermListSteps creates a term and verifies its URN appears in the
// datahub_glossary_terms enumeration data source.
func GlossaryTermListSteps(nodeID, termID string) []resource.TestStep {
	termURN := "urn:li:glossaryTerm:" + termID
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_glossary_node" "parent" {
  node_id = %q
  name    = "Finance"
}

resource "datahub_glossary_term" "test" {
  term_id     = %q
  name        = "EBITDA"
  parent_node = datahub_glossary_node.parent.urn
}

data "datahub_glossary_terms" "all" {
  depends_on = [datahub_glossary_term.test]
}
`, nodeID, termID)

	return []resource.TestStep{
		{
			Config: cfg,
			Check: resource.ComposeAggregateTestCheckFunc(
				assertURNInList("data.datahub_glossary_terms.all", termURN),
			),
		},
	}
}

// GlossaryTermCheckDestroy verifies every datahub_glossary_term in the
// post-destroy state has been removed from DataHub.
func GlossaryTermCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_glossary_term" {
			continue
		}
		urn := rs.Primary.Attributes["urn"]
		if urn == "" {
			urn = rs.Primary.ID
		}
		term, getErr := client.GetGlossaryTermByURN(ctx, urn)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking datahub_glossary_term %q: %w", urn, getErr)
		}
		if term != nil {
			return fmt.Errorf("datahub_glossary_term %q still exists after destroy", urn)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tag scenarios
// ---------------------------------------------------------------------------

// TagLifecycleSteps returns test steps covering create (with description and
// colour), in-place rename (exercises the tagProperties aspect-write path),
// description and colour update, and import (by id and by URN) for
// datahub_tag.
//
// tagID is the tag_id attribute and must be unique within the target DataHub
// instance.
func TagLifecycleSteps(tagID string) []resource.TestStep {
	const addr = "datahub_tag.test"
	urn := "urn:li:tag:" + tagID

	return []resource.TestStep{
		{
			// Create a tag with name, description, and colour.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_tag" "test" {
  tag_id      = %q
  name        = "Sensitive"
  description = "Marks sensitive data assets"
  color_hex   = "#FF6B6B"
}
`, tagID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("tag_id"), knownvalue.StringExact(tagID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Sensitive")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("Marks sensitive data assets")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("color_hex"), knownvalue.StringExact("#FF6B6B")),
			},
		},
		{
			// Rename in place -- exercises WriteTagProperties (tagProperties aspect write).
			// Must not trigger replacement.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_tag" "test" {
  tag_id      = %q
  name        = "Highly Sensitive"
  description = "Marks sensitive data assets"
  color_hex   = "#FF6B6B"
}
`, tagID),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Highly Sensitive")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("color_hex"), knownvalue.StringExact("#FF6B6B")),
			},
		},
		{
			// Update description and colour independently (no rename).
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_tag" "test" {
  tag_id      = %q
  name        = "Highly Sensitive"
  description = "Marks highly sensitive data requiring special handling"
  color_hex   = "#C0392B"
}
`, tagID),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("Marks highly sensitive data requiring special handling")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("color_hex"), knownvalue.StringExact("#C0392B")),
			},
		},
		{
			// Import by bare tag_id.
			ResourceName:      addr,
			ImportState:       true,
			ImportStateId:     tagID,
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

// TagDataSourceSteps seeds a tag via the resource then reads it back via the
// singular datahub_tag data source.
func TagDataSourceSteps(tagID string) []resource.TestStep {
	const addr = "data.datahub_tag.test"
	urn := "urn:li:tag:" + tagID

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_tag" "seed" {
  tag_id      = %q
  name        = "Lookup Tag"
  description = "looked up"
  color_hex   = "#3498DB"
}

data "datahub_tag" "test" {
  tag_id = datahub_tag.seed.tag_id
}
`, tagID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("tag_id"), knownvalue.StringExact(tagID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Lookup Tag")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("looked up")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("color_hex"), knownvalue.StringExact("#3498DB")),
			},
		},
	}
}

// TagListSteps creates a tag and verifies its URN appears in the datahub_tags
// enumeration data source.
func TagListSteps(tagID string) []resource.TestStep {
	urn := "urn:li:tag:" + tagID
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_tag" "test" {
  tag_id = %q
  name   = "List Tag"
}

data "datahub_tags" "all" {
  depends_on = [datahub_tag.test]
}
`, tagID)

	return []resource.TestStep{
		{
			Config: cfg,
			Check: resource.ComposeAggregateTestCheckFunc(
				assertURNInList("data.datahub_tags.all", urn),
			),
		},
	}
}

// TagCheckDestroy verifies every datahub_tag in the post-destroy state has
// been removed from DataHub.
func TagCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_tag" {
			continue
		}
		urn := rs.Primary.Attributes["urn"]
		if urn == "" {
			urn = rs.Primary.ID
		}
		tag, getErr := client.GetTagByURN(ctx, urn)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking datahub_tag %q: %w", urn, getErr)
		}
		if tag != nil {
			return fmt.Errorf("datahub_tag %q still exists after destroy", urn)
		}
	}
	return nil
}

// StructuredPropertyLifecycleSteps exercises the full resource lifecycle:
//   - Create with value_type, entity_types, allowed_values, and settings.
//   - Additive update: add an entity_type + add an allowed_value + widen
//     cardinality + update display_name/description/settings. These must NOT
//     trigger replacement (ResourceActionUpdate).
//   - Shrink update: remove an allowed_value. Must trigger replacement
//     (ResourceActionDestroyBeforeCreate).
//   - Import by bare property_id.
//   - Import by full URN.
func StructuredPropertyLifecycleSteps(propertyID string) []resource.TestStep {
	const addr = "datahub_structured_property.test"
	urn := "urn:li:structuredProperty:" + propertyID

	return []resource.TestStep{
		{
			// Create: string-typed, single-valued, applies to datasets, with two
			// allowed values and search-filter enabled.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_structured_property" "test" {
  property_id  = %q
  value_type   = "string"
  cardinality  = "SINGLE"
  entity_types = ["dataset"]

  display_name = "Data Classification"
  description  = "Classifies data sensitivity"

  allowed_values = [
    { string_value = "Public",   description = "Publicly accessible data" },
    { string_value = "Internal", description = "Internal use only" },
  ]

  settings = {
    show_in_search_filters = true
  }
}
`, propertyID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("property_id"), knownvalue.StringExact(propertyID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("value_type"), knownvalue.StringExact("string")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("cardinality"), knownvalue.StringExact("SINGLE")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("display_name"), knownvalue.StringExact("Data Classification")),
			},
		},
		{
			// Additive update: add dashboard to entity_types, add a third allowed
			// value, widen cardinality to MULTIPLE, update display_name and settings.
			// Must NOT trigger replacement.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_structured_property" "test" {
  property_id  = %q
  value_type   = "string"
  cardinality  = "MULTIPLE"
  entity_types = ["dataset", "dashboard"]

  display_name = "Data Classification (updated)"
  description  = "Classifies data sensitivity and audience"

  allowed_values = [
    { string_value = "Public",      description = "Publicly accessible data" },
    { string_value = "Internal",    description = "Internal use only" },
    { string_value = "Confidential", description = "Restricted access required" },
  ]

  settings = {
    show_in_search_filters = true
    show_in_asset_summary  = true
  }
}
`, propertyID),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("cardinality"), knownvalue.StringExact("MULTIPLE")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("display_name"), knownvalue.StringExact("Data Classification (updated)")),
			},
		},
		{
			// Shrink: remove one allowed_value. The DataHub API cannot remove
			// allowed values, so Terraform must replace the resource.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_structured_property" "test" {
  property_id  = %q
  value_type   = "string"
  cardinality  = "MULTIPLE"
  entity_types = ["dataset", "dashboard"]

  display_name = "Data Classification (updated)"
  description  = "Classifies data sensitivity and audience"

  allowed_values = [
    { string_value = "Public",   description = "Publicly accessible data" },
    { string_value = "Internal", description = "Internal use only" },
  ]

  settings = {
    show_in_search_filters = true
    show_in_asset_summary  = true
  }
}
`, propertyID),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroyBeforeCreate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
			},
		},
		{
			// Import by bare property_id.
			ResourceName:      addr,
			ImportState:       true,
			ImportStateId:     propertyID,
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

// StructuredPropertyDataSourceSteps seeds a structured property via the resource
// then reads it back via the singular datahub_structured_property data source.
func StructuredPropertyDataSourceSteps(propertyID string) []resource.TestStep {
	const addr = "data.datahub_structured_property.test"
	urn := "urn:li:structuredProperty:" + propertyID

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_structured_property" "seed" {
  property_id  = %q
  value_type   = "number"
  entity_types = ["dataset"]
  display_name = "Retention Days"
  description  = "Data retention period in days"
}

data "datahub_structured_property" "test" {
  property_id = datahub_structured_property.seed.property_id
}
`, propertyID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("property_id"), knownvalue.StringExact(propertyID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("value_type"), knownvalue.StringExact("number")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("display_name"), knownvalue.StringExact("Retention Days")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("Data retention period in days")),
			},
		},
	}
}

// StructuredPropertyListSteps creates a structured property and verifies its
// URN appears in the datahub_structured_properties enumeration data source.
func StructuredPropertyListSteps(propertyID string) []resource.TestStep {
	urn := "urn:li:structuredProperty:" + propertyID
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_structured_property" "test" {
  property_id  = %q
  value_type   = "string"
  entity_types = ["dataset"]
}

data "datahub_structured_properties" "all" {
  depends_on = [datahub_structured_property.test]
}
`, propertyID)

	return []resource.TestStep{
		{
			Config: cfg,
			Check: resource.ComposeAggregateTestCheckFunc(
				assertURNInList("data.datahub_structured_properties.all", urn),
			),
		},
	}
}

// StructuredPropertyCheckDestroy verifies every datahub_structured_property in
// the post-destroy state has been removed from DataHub.
func StructuredPropertyCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_structured_property" {
			continue
		}
		urn := rs.Primary.Attributes["urn"]
		if urn == "" {
			urn = rs.Primary.ID
		}
		sp, getErr := client.GetStructuredPropertyByURN(ctx, urn)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking datahub_structured_property %q: %w", urn, getErr)
		}
		if sp != nil {
			return fmt.Errorf("datahub_structured_property %q still exists after destroy", urn)
		}
	}
	return nil
}

// OwnershipTypeLifecycleSteps exercises the full resource lifecycle for
// datahub_ownership_type: create with description, in-place update, and import
// by both bare type_id and full URN.
func OwnershipTypeLifecycleSteps(typeID string) []resource.TestStep {
	const addr = "datahub_ownership_type.test"
	urn := "urn:li:ownershipType:" + typeID

	return []resource.TestStep{
		{
			// Create with name and description.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_ownership_type" "test" {
  type_id     = %q
  name        = "Data Quality Lead"
  description = "Responsible for data quality and validation"
}
`, typeID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("type_id"), knownvalue.StringExact(typeID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Data Quality Lead")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("Responsible for data quality and validation")),
			},
		},
		{
			// Update name and description in place -- must not trigger replacement.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_ownership_type" "test" {
  type_id     = %q
  name        = "Data Quality Owner"
  description = "Owns data quality processes and remediation"
}
`, typeID),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Data Quality Owner")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("Owns data quality processes and remediation")),
			},
		},
		{
			// Remove description (optional field) -- must not trigger replacement.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_ownership_type" "test" {
  type_id = %q
  name    = "Data Quality Owner"
}
`, typeID),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Data Quality Owner")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.Null()),
			},
		},
		{
			// Import by bare type_id.
			ResourceName:      addr,
			ImportState:       true,
			ImportStateId:     typeID,
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

// OwnershipTypeDataSourceSteps seeds an ownership type via the resource then
// reads it back via the singular datahub_ownership_type data source.
func OwnershipTypeDataSourceSteps(typeID string) []resource.TestStep {
	const addr = "data.datahub_ownership_type.test"
	urn := "urn:li:ownershipType:" + typeID

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_ownership_type" "seed" {
  type_id     = %q
  name        = "Lookup Ownership Type"
  description = "looked up"
}

data "datahub_ownership_type" "test" {
  type_id = datahub_ownership_type.seed.type_id
}
`, typeID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("type_id"), knownvalue.StringExact(typeID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Lookup Ownership Type")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("looked up")),
			},
		},
	}
}

// OwnershipTypeListSteps creates an ownership type and verifies its URN appears
// in the datahub_ownership_types enumeration data source.
func OwnershipTypeListSteps(typeID string) []resource.TestStep {
	urn := "urn:li:ownershipType:" + typeID
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_ownership_type" "test" {
  type_id = %q
  name    = "List Ownership Type"
}

data "datahub_ownership_types" "all" {
  depends_on = [datahub_ownership_type.test]
}
`, typeID)

	return []resource.TestStep{
		{
			Config: cfg,
			Check: resource.ComposeAggregateTestCheckFunc(
				assertURNInList("data.datahub_ownership_types.all", urn),
			),
		},
	}
}

// OwnershipTypeCheckDestroy verifies every datahub_ownership_type in the
// post-destroy state has been removed from DataHub.
func OwnershipTypeCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_ownership_type" {
			continue
		}
		urn := rs.Primary.Attributes["urn"]
		if urn == "" {
			urn = rs.Primary.ID
		}
		ot, getErr := client.GetOwnershipTypeByURN(ctx, urn)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking datahub_ownership_type %q: %w", urn, getErr)
		}
		if ot != nil {
			return fmt.Errorf("datahub_ownership_type %q still exists after destroy", urn)
		}
	}
	return nil
}

// DataProductLifecycleSteps exercises the full resource lifecycle for
// datahub_data_product: create with all optional fields, in-place update,
// removal of optional fields, and import by both bare data_product_id and
// full URN.
func DataProductLifecycleSteps(dataProductID, domainID string) []resource.TestStep {
	const addr = "datahub_data_product.test"
	const domainAddr = "datahub_domain.test"
	urn := "urn:li:dataProduct:" + dataProductID
	domainURN := "urn:li:domain:" + domainID

	return []resource.TestStep{
		{
			// Create with all optional fields set.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_domain" "test" {
  domain_id = %q
  name      = "TF Test Domain"
}

resource "datahub_data_product" "test" {
  data_product_id   = %q
  name              = "Orders v2"
  description       = "Primary orders data product"
  external_url      = "https://example.com/docs/orders"
  domain            = datahub_domain.test.urn
  custom_properties = { tier = "gold", team = "platform" }
}
`, domainID, dataProductID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("data_product_id"), knownvalue.StringExact(dataProductID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Orders v2")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("Primary orders data product")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("external_url"), knownvalue.StringExact("https://example.com/docs/orders")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("domain"), knownvalue.StringExact(domainURN)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties"), knownvalue.MapExact(map[string]knownvalue.Check{
					"tier": knownvalue.StringExact("gold"),
					"team": knownvalue.StringExact("platform"),
				})),
				statecheck.ExpectKnownValue(domainAddr, tfjsonpath.New("urn"), knownvalue.StringExact(domainURN)),
			},
		},
		{
			// Update name and description in place -- must not trigger replacement.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_domain" "test" {
  domain_id = %q
  name      = "TF Test Domain"
}

resource "datahub_data_product" "test" {
  data_product_id   = %q
  name              = "Orders v2 (updated)"
  description       = "Updated description"
  external_url      = "https://example.com/docs/orders-v2"
  domain            = datahub_domain.test.urn
  custom_properties = { tier = "platinum" }
}
`, domainID, dataProductID),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Orders v2 (updated)")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("Updated description")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("external_url"), knownvalue.StringExact("https://example.com/docs/orders-v2")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties"), knownvalue.MapExact(map[string]knownvalue.Check{
					"tier": knownvalue.StringExact("platinum"),
				})),
			},
		},
		{
			// Remove all optional fields -- must not trigger replacement.
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_domain" "test" {
  domain_id = %q
  name      = "TF Test Domain"
}

resource "datahub_data_product" "test" {
  data_product_id = %q
  name            = "Orders v2 (updated)"
}
`, domainID, dataProductID),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Orders v2 (updated)")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.Null()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("external_url"), knownvalue.Null()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("domain"), knownvalue.Null()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties"), knownvalue.Null()),
			},
		},
		{
			// Import by bare data_product_id.
			ResourceName:      addr,
			ImportState:       true,
			ImportStateId:     dataProductID,
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

// DataProductDataSourceSteps seeds a data product via the resource then reads
// it back via the singular datahub_data_product data source.
func DataProductDataSourceSteps(dataProductID, domainID string) []resource.TestStep {
	const addr = "data.datahub_data_product.test"
	urn := "urn:li:dataProduct:" + dataProductID

	return []resource.TestStep{
		{
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_domain" "test" {
  domain_id = %q
  name      = "TF Test Domain DS"
}

resource "datahub_data_product" "seed" {
  data_product_id = %q
  name            = "Lookup Data Product"
  description     = "looked up"
  domain          = datahub_domain.test.urn
}

data "datahub_data_product" "test" {
  data_product_id = datahub_data_product.seed.data_product_id
}
`, domainID, dataProductID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("data_product_id"), knownvalue.StringExact(dataProductID)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("Lookup Data Product")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("looked up")),
			},
		},
	}
}

// DataProductListSteps creates a data product and verifies its URN appears in
// the datahub_data_products enumeration data source.
func DataProductListSteps(dataProductID, domainID string) []resource.TestStep {
	urn := "urn:li:dataProduct:" + dataProductID
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_domain" "test" {
  domain_id = %q
  name      = "TF Test Domain List"
}

resource "datahub_data_product" "test" {
  data_product_id = %q
  name            = "List Data Product"
  domain          = datahub_domain.test.urn
}

data "datahub_data_products" "all" {
  depends_on = [datahub_data_product.test]
}
`, domainID, dataProductID)

	return []resource.TestStep{
		{
			Config: cfg,
			Check: resource.ComposeAggregateTestCheckFunc(
				assertURNInList("data.datahub_data_products.all", urn),
			),
		},
	}
}

// DataProductCheckDestroy verifies every datahub_data_product in the
// post-destroy state has been removed from DataHub.
func DataProductCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_data_product" {
			continue
		}
		urn := rs.Primary.Attributes["urn"]
		if urn == "" {
			urn = rs.Primary.ID
		}
		dp, getErr := client.GetDataProductByURN(ctx, urn)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking datahub_data_product %q: %w", urn, getErr)
		}
		if dp != nil {
			return fmt.Errorf("datahub_data_product %q still exists after destroy", urn)
		}
	}
	return nil
}

// CustomAssertionLifecycleSteps returns test steps covering create, update,
// and import for datahub_custom_assertion.
func CustomAssertionLifecycleSteps() []resource.TestStep {
	const addr = "datahub_custom_assertion.test"

	return []resource.TestStep{
		{
			Config: providerBlock + `
resource "datahub_custom_assertion" "test" {
  entity_urn     = "urn:li:dataset:(urn:li:dataPlatform:hive,test.table,PROD)"
  assertion_type = "CUSTOM"
  description    = "TF Example - custom assertion"
  platform_urn   = "urn:li:dataPlatform:dbt"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("assertion_type"), knownvalue.StringExact("CUSTOM")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("entity_urn"), knownvalue.StringExact("urn:li:dataset:(urn:li:dataPlatform:hive,test.table,PROD)")),
			},
		},
		{
			Config: providerBlock + `
resource "datahub_custom_assertion" "test" {
  entity_urn     = "urn:li:dataset:(urn:li:dataPlatform:hive,test.table,PROD)"
  assertion_type = "CUSTOM"
  description    = "TF Example - updated description"
  platform_urn   = "urn:li:dataPlatform:dbt"
  logic          = "SELECT COUNT(*) FROM test.table WHERE value < 0"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("TF Example - updated description")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("logic"), knownvalue.StringExact("SELECT COUNT(*) FROM test.table WHERE value < 0")),
			},
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
				},
			},
		},
		{
			ResourceName:      addr,
			ImportState:       true,
			ImportStateVerify: true,
		},
	}
}

// CustomAssertionDataSourceSteps returns test steps for the datahub_assertion data source.
func CustomAssertionDataSourceSteps() []resource.TestStep {
	const rAddr = "datahub_custom_assertion.ds_test"
	const dsAddr = "data.datahub_assertion.lookup"

	return []resource.TestStep{
		{
			Config: providerBlock + `
resource "datahub_custom_assertion" "ds_test" {
  entity_urn     = "urn:li:dataset:(urn:li:dataPlatform:hive,ds.table,PROD)"
  assertion_type = "CUSTOM"
  description    = "data source lookup test"
  platform_urn   = "urn:li:dataPlatform:great_expectations"
}

data "datahub_assertion" "lookup" {
  urn = datahub_custom_assertion.ds_test.urn
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(dsAddr, tfjsonpath.New("urn"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue(dsAddr, tfjsonpath.New("assertion_type"), knownvalue.StringExact("CUSTOM")),
				statecheck.ExpectKnownValue(dsAddr, tfjsonpath.New("entity_urn"), knownvalue.StringExact("urn:li:dataset:(urn:li:dataPlatform:hive,ds.table,PROD)")),
			},
		},
		// Avoid an unused variable warning: ensure rAddr resource is referenced.
		{
			Config: providerBlock + `
resource "datahub_custom_assertion" "ds_test" {
  entity_urn     = "urn:li:dataset:(urn:li:dataPlatform:hive,ds.table,PROD)"
  assertion_type = "CUSTOM"
  description    = "data source lookup test"
  platform_urn   = "urn:li:dataPlatform:great_expectations"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(rAddr, tfjsonpath.New("urn"), knownvalue.NotNull()),
			},
		},
	}
}

// CustomAssertionListSteps returns test steps for the datahub_assertions plural data source.
func CustomAssertionListSteps() []resource.TestStep {
	const dsAddr = "data.datahub_assertions.all"

	return []resource.TestStep{
		{
			Config: providerBlock + `
resource "datahub_custom_assertion" "list_test" {
  entity_urn     = "urn:li:dataset:(urn:li:dataPlatform:hive,list.table,PROD)"
  assertion_type = "CUSTOM"
  description    = "list test"
  platform_urn   = "urn:li:dataPlatform:dbt"
}

data "datahub_assertions" "all" {
  depends_on = [datahub_custom_assertion.list_test]
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(dsAddr, tfjsonpath.New("urns"), knownvalue.ListSizeExact(1)),
			},
		},
	}
}

// CustomAssertionCheckDestroy verifies every datahub_custom_assertion in the
// post-destroy state has been removed from DataHub.
func CustomAssertionCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_custom_assertion" {
			continue
		}
		urn := rs.Primary.Attributes["urn"]
		if urn == "" {
			urn = rs.Primary.ID
		}
		a, getErr := client.GetAssertionByURN(ctx, urn)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking datahub_custom_assertion %q: %w", urn, getErr)
		}
		if a != nil {
			return fmt.Errorf("datahub_custom_assertion %q still exists after destroy", urn)
		}
	}
	return nil
}

// VolumeAssertionLifecycleSteps returns test steps for datahub_volume_assertion.
func VolumeAssertionLifecycleSteps() []resource.TestStep {
	const addr = "datahub_volume_assertion.test"

	return []resource.TestStep{
		{
			Config: providerBlock + `
resource "datahub_volume_assertion" "test" {
  entity_urn          = "urn:li:dataset:(urn:li:dataPlatform:sqlite,tf_assertion_test.tf_test_data,PROD)"
  volume_type         = "ROW_COUNT_TOTAL"
  operator            = "GREATER_THAN_OR_EQUAL_TO"
  single_value        = "100"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  source_type         = "DATAHUB_DATASET_PROFILE"
  mode                = "ACTIVE"
  on_success_actions  = ["RESOLVE_INCIDENT"]
  on_failure_actions  = ["RAISE_INCIDENT"]
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("volume_type"), knownvalue.StringExact("ROW_COUNT_TOTAL")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("operator"), knownvalue.StringExact("GREATER_THAN_OR_EQUAL_TO")),
			},
		},
		{
			Config: providerBlock + `
resource "datahub_volume_assertion" "test" {
  entity_urn          = "urn:li:dataset:(urn:li:dataPlatform:sqlite,tf_assertion_test.tf_test_data,PROD)"
  volume_type         = "ROW_COUNT_TOTAL"
  operator            = "BETWEEN"
  min_value           = "80"
  max_value           = "200"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  source_type         = "DATAHUB_DATASET_PROFILE"
  mode                = "ACTIVE"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("operator"), knownvalue.StringExact("BETWEEN")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("min_value"), knownvalue.StringExact("80")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("max_value"), knownvalue.StringExact("200")),
			},
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectNonEmptyPlan(),
				},
			},
		},
		{
			ResourceName:            addr,
			ImportState:             true,
			ImportStateVerify:       true,
			ImportStateVerifyIgnore: []string{"evaluation_cron", "evaluation_timezone", "source_type", "mode", "executor_id"},
		},
	}
}

// VolumeAssertionCheckDestroy verifies every datahub_volume_assertion has been removed.
func VolumeAssertionCheckDestroy(s *terraform.State) error {
	return assertionCheckDestroy(s, "datahub_volume_assertion")
}

// VolumeAssertionChangeLifecycleSteps returns test steps for the ROW_COUNT_CHANGE
// (growth) volume assertion sub-type: create an ABSOLUTE single-value change
// assertion, update it to a PERCENTAGE BETWEEN range, then import and verify.
func VolumeAssertionChangeLifecycleSteps() []resource.TestStep {
	const addr = "datahub_volume_assertion.test"
	const entity = "urn:li:dataset:(urn:li:dataPlatform:sqlite,tf_assertion_test.tf_test_data,PROD)"

	return []resource.TestStep{
		{
			Config: providerBlock + `
resource "datahub_volume_assertion" "test" {
  entity_urn          = "` + entity + `"
  volume_type         = "ROW_COUNT_CHANGE"
  change_type         = "ABSOLUTE"
  operator            = "GREATER_THAN_OR_EQUAL_TO"
  single_value        = "10"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  source_type         = "DATAHUB_DATASET_PROFILE"
  mode                = "ACTIVE"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("volume_type"), knownvalue.StringExact("ROW_COUNT_CHANGE")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("change_type"), knownvalue.StringExact("ABSOLUTE")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("operator"), knownvalue.StringExact("GREATER_THAN_OR_EQUAL_TO")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("single_value"), knownvalue.StringExact("10")),
			},
		},
		{
			Config: providerBlock + `
resource "datahub_volume_assertion" "test" {
  entity_urn          = "` + entity + `"
  volume_type         = "ROW_COUNT_CHANGE"
  change_type         = "PERCENTAGE"
  operator            = "BETWEEN"
  min_value           = "5"
  max_value           = "25"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  source_type         = "DATAHUB_DATASET_PROFILE"
  mode                = "ACTIVE"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("change_type"), knownvalue.StringExact("PERCENTAGE")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("operator"), knownvalue.StringExact("BETWEEN")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("min_value"), knownvalue.StringExact("5")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("max_value"), knownvalue.StringExact("25")),
			},
		},
		{
			ResourceName:            addr,
			ImportState:             true,
			ImportStateVerify:       true,
			ImportStateVerifyIgnore: []string{"evaluation_cron", "evaluation_timezone", "source_type", "mode", "executor_id"},
		},
	}
}

// VolumeAssertionChangeTypeValidationSteps returns config-only steps that assert
// the change_type/volume_type pairing rules are enforced at plan time:
// ROW_COUNT_CHANGE requires change_type, and ROW_COUNT_TOTAL rejects it.
func VolumeAssertionChangeTypeValidationSteps() []resource.TestStep {
	const entity = "urn:li:dataset:(urn:li:dataPlatform:sqlite,tf_assertion_test.tf_test_data,PROD)"

	return []resource.TestStep{
		{
			Config: providerBlock + `
resource "datahub_volume_assertion" "test" {
  entity_urn          = "` + entity + `"
  volume_type         = "ROW_COUNT_CHANGE"
  operator            = "GREATER_THAN_OR_EQUAL_TO"
  single_value        = "10"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  source_type         = "DATAHUB_DATASET_PROFILE"
  mode                = "ACTIVE"
}
`,
			ExpectError: regexp.MustCompile(`change_type is required`),
			PlanOnly:    true,
		},
		{
			Config: providerBlock + `
resource "datahub_volume_assertion" "test" {
  entity_urn          = "` + entity + `"
  volume_type         = "ROW_COUNT_TOTAL"
  change_type         = "ABSOLUTE"
  operator            = "GREATER_THAN_OR_EQUAL_TO"
  single_value        = "10"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  source_type         = "DATAHUB_DATASET_PROFILE"
  mode                = "ACTIVE"
}
`,
			ExpectError: regexp.MustCompile(`change_type is only valid`),
			PlanOnly:    true,
		},
	}
}

// FreshnessAssertionLifecycleSteps returns test steps for datahub_freshness_assertion.
func FreshnessAssertionLifecycleSteps() []resource.TestStep {
	const addr = "datahub_freshness_assertion.test"

	return []resource.TestStep{
		{
			Config: providerBlock + `
resource "datahub_freshness_assertion" "test" {
  entity_urn              = "urn:li:dataset:(urn:li:dataPlatform:hive,freshness.table,PROD)"
  schedule_type           = "FIXED_INTERVAL"
  fixed_interval_unit     = "HOUR"
  fixed_interval_multiple = 24
  evaluation_cron         = "0 */8 * * *"
  evaluation_timezone     = "UTC"
  source_type             = "DATAHUB_OPERATION"
  mode                    = "ACTIVE"
  on_success_actions      = ["RESOLVE_INCIDENT"]
  on_failure_actions      = ["RAISE_INCIDENT"]
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("schedule_type"), knownvalue.StringExact("FIXED_INTERVAL")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("fixed_interval_unit"), knownvalue.StringExact("HOUR")),
			},
		},
		{
			ResourceName:            addr,
			ImportState:             true,
			ImportStateVerify:       true,
			ImportStateVerifyIgnore: []string{"evaluation_cron", "evaluation_timezone", "source_type", "mode", "executor_id"},
		},
	}
}

// FreshnessAssertionCheckDestroy verifies every datahub_freshness_assertion has been removed.
func FreshnessAssertionCheckDestroy(s *terraform.State) error {
	return assertionCheckDestroy(s, "datahub_freshness_assertion")
}

// SQLAssertionLifecycleSteps returns test steps for datahub_sql_assertion.
func SQLAssertionLifecycleSteps() []resource.TestStep {
	const addr = "datahub_sql_assertion.test"

	return []resource.TestStep{
		{
			Config: providerBlock + `
resource "datahub_sql_assertion" "test" {
  entity_urn          = "urn:li:dataset:(urn:li:dataPlatform:bigquery,project.dataset.table,PROD)"
  sql_type            = "METRIC"
  statement           = "SELECT COUNT(*) FROM project.dataset.table WHERE value < 0"
  operator            = "EQUAL_TO"
  value               = "0"
  description         = "no negative values"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  mode                = "ACTIVE"
  on_success_actions  = ["RESOLVE_INCIDENT"]
  on_failure_actions  = ["RAISE_INCIDENT"]
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("sql_type"), knownvalue.StringExact("METRIC")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("operator"), knownvalue.StringExact("EQUAL_TO")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("value"), knownvalue.StringExact("0")),
			},
		},
		{
			ResourceName:            addr,
			ImportState:             true,
			ImportStateVerify:       true,
			ImportStateVerifyIgnore: []string{"evaluation_cron", "evaluation_timezone", "mode", "executor_id"},
		},
	}
}

// SQLAssertionCheckDestroy verifies every datahub_sql_assertion has been removed.
func SQLAssertionCheckDestroy(s *terraform.State) error {
	return assertionCheckDestroy(s, "datahub_sql_assertion")
}

// SQLAssertionChangeLifecycleSteps returns test steps for the METRIC_CHANGE sql
// assertion sub-type: create an ABSOLUTE change assertion, update the change type
// to PERCENTAGE, then import and verify.
func SQLAssertionChangeLifecycleSteps() []resource.TestStep {
	const addr = "datahub_sql_assertion.test"
	const entity = "urn:li:dataset:(urn:li:dataPlatform:bigquery,project.dataset.table,PROD)"

	return []resource.TestStep{
		{
			Config: providerBlock + `
resource "datahub_sql_assertion" "test" {
  entity_urn          = "` + entity + `"
  sql_type            = "METRIC_CHANGE"
  change_type         = "ABSOLUTE"
  statement           = "SELECT COUNT(*) FROM project.dataset.table"
  operator            = "GREATER_THAN_OR_EQUAL_TO"
  value               = "10"
  description         = "row count must keep growing"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  mode                = "ACTIVE"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("sql_type"), knownvalue.StringExact("METRIC_CHANGE")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("change_type"), knownvalue.StringExact("ABSOLUTE")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("value"), knownvalue.StringExact("10")),
			},
		},
		{
			Config: providerBlock + `
resource "datahub_sql_assertion" "test" {
  entity_urn          = "` + entity + `"
  sql_type            = "METRIC_CHANGE"
  change_type         = "PERCENTAGE"
  statement           = "SELECT COUNT(*) FROM project.dataset.table"
  operator            = "LESS_THAN"
  value               = "50"
  description         = "row count must not balloon"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  mode                = "ACTIVE"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("change_type"), knownvalue.StringExact("PERCENTAGE")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("operator"), knownvalue.StringExact("LESS_THAN")),
			},
		},
		{
			ResourceName:            addr,
			ImportState:             true,
			ImportStateVerify:       true,
			ImportStateVerifyIgnore: []string{"evaluation_cron", "evaluation_timezone", "mode", "executor_id"},
		},
	}
}

// SQLAssertionChangeTypeValidationSteps returns config-only steps asserting the
// METRIC_CHANGE pairing rules: change_type required for METRIC_CHANGE, rejected
// for METRIC, and description required for METRIC_CHANGE.
func SQLAssertionChangeTypeValidationSteps() []resource.TestStep {
	const entity = "urn:li:dataset:(urn:li:dataPlatform:bigquery,project.dataset.table,PROD)"

	return []resource.TestStep{
		{
			Config: providerBlock + `
resource "datahub_sql_assertion" "test" {
  entity_urn          = "` + entity + `"
  sql_type            = "METRIC_CHANGE"
  statement           = "SELECT COUNT(*) FROM project.dataset.table"
  operator            = "GREATER_THAN"
  value               = "10"
  description         = "needs change_type"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  mode                = "ACTIVE"
}
`,
			ExpectError: regexp.MustCompile(`change_type is required`),
			PlanOnly:    true,
		},
		{
			Config: providerBlock + `
resource "datahub_sql_assertion" "test" {
  entity_urn          = "` + entity + `"
  sql_type            = "METRIC"
  change_type         = "ABSOLUTE"
  statement           = "SELECT COUNT(*) FROM project.dataset.table"
  operator            = "EQUAL_TO"
  value               = "0"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  mode                = "ACTIVE"
}
`,
			ExpectError: regexp.MustCompile(`change_type is only valid`),
			PlanOnly:    true,
		},
		{
			Config: providerBlock + `
resource "datahub_sql_assertion" "test" {
  entity_urn          = "` + entity + `"
  sql_type            = "METRIC_CHANGE"
  change_type         = "ABSOLUTE"
  statement           = "SELECT COUNT(*) FROM project.dataset.table"
  operator            = "GREATER_THAN"
  value               = "10"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  mode                = "ACTIVE"
}
`,
			ExpectError: regexp.MustCompile(`description is required`),
			PlanOnly:    true,
		},
	}
}

// assertionCheckDestroy is a shared helper for assertion CheckDestroy functions.
func assertionCheckDestroy(s *terraform.State, resourceType string) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != resourceType {
			continue
		}
		urn := rs.Primary.Attributes["urn"]
		if urn == "" {
			urn = rs.Primary.ID
		}
		a, getErr := client.GetAssertionByURN(ctx, urn)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking %s %q: %w", resourceType, urn, getErr)
		}
		if a != nil {
			return fmt.Errorf("%s %q still exists after destroy", resourceType, urn)
		}
	}
	return nil
}

// SeedAssertion injects an assertion into the mock store at baseURL via the
// /test-control/seed-assertion endpoint. Used to create assertions the normal
// API cannot -- e.g. an EXTERNAL (ingested) assertion. Mock-only.
func SeedAssertion(baseURL, urn, typ, source, subType string) {
	body := fmt.Sprintf(`{"urn":%q,"type":%q,"source":%q,"subType":%q}`, urn, typ, source, subType)
	resp, err := http.Post(baseURL+"/test-control/seed-assertion", "application/json", strings.NewReader(body)) //nolint:noctx
	if err != nil {
		panic(fmt.Sprintf("SeedAssertion: %v", err))
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		panic(fmt.Sprintf("SeedAssertion: unexpected status %d", resp.StatusCode))
	}
}

// VolumeAssertionImportGuardSteps verifies that a direct `terraform import` of a
// non-NATIVE assertion (an ingested EXTERNAL one here) into datahub_volume_assertion
// is refused at point-of-import by the resource's source guard. Mock-only: it
// seeds the EXTERNAL assertion via /test-control/seed-assertion, which a live
// target does not expose.
func VolumeAssertionImportGuardSteps() []resource.TestStep {
	const addr = "datahub_volume_assertion.test"
	const externalURN = "urn:li:assertion:external-volume-import-guard"
	cfg := providerBlock + `
resource "datahub_volume_assertion" "test" {
  entity_urn          = "urn:li:dataset:(urn:li:dataPlatform:sqlite,tf_assertion_test.tf_test_data,PROD)"
  volume_type         = "ROW_COUNT_TOTAL"
  operator            = "GREATER_THAN_OR_EQUAL_TO"
  single_value        = "100"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  source_type         = "DATAHUB_DATASET_PROFILE"
  mode                = "ACTIVE"
}
`
	return []resource.TestStep{
		// Step 1: create a NATIVE assertion so the address exists and CheckDestroy
		// has something to clean up.
		{Config: cfg},
		// Step 2: seed an EXTERNAL (ingested) volume assertion and attempt to
		// import it into the same resource address. The Read source guard, which
		// runs as part of ImportState, must refuse it.
		{
			PreConfig: func() {
				SeedAssertion(os.Getenv("DATAHUB_GMS_URL"), externalURN, "VOLUME", "EXTERNAL", "ROW_COUNT_TOTAL")
			},
			ResourceName:  addr,
			ImportState:   true,
			ImportStateId: externalURN,
			ExpectError:   regexp.MustCompile(`not Terraform-manageable`),
		},
		// Step 3: re-apply so the test framework can destroy cleanly.
		{Config: cfg},
	}
}
