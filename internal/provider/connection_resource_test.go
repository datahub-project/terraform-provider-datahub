// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_Connection_DatabricksLifecycle exercises the full lifecycle of
// datahub_connection with a databricks block:
//   - Create (verify URN, name, platform)
//   - Update name in-place (no replace because connection_id is unchanged)
//   - Import by URN
func TestAcc_Connection_DatabricksLifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	connectionID := tg.Name("tfprovider-databricks-conn")

	resource.Test(t, resource.TestCase{
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_11_0),
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.ConnectionCheckDestroy,
		Steps:                    datahubtesting.ConnectionDatabricksLifecycleSteps(connectionID),
	})
}

// TestAcc_Connection_Snowflake exercises create/destroy for a snowflake block.
func TestAcc_Connection_Snowflake(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	connectionID := tg.Name("tfprovider-snowflake-conn")

	resource.Test(t, resource.TestCase{
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_11_0),
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.ConnectionCheckDestroy,
		Steps:                    datahubtesting.ConnectionSnowflakeSteps(connectionID),
	})
}

// TestAcc_Connection_RawConfig exercises the raw_config escape hatch.
func TestAcc_Connection_RawConfig(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	connectionID := tg.Name("tfprovider-raw-conn")

	resource.Test(t, resource.TestCase{
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_11_0),
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.ConnectionCheckDestroy,
		Steps:                    datahubtesting.ConnectionRawConfigSteps(connectionID),
	})
}

// TestAcc_Connection_VersionBump verifies that incrementing config_wo_version
// triggers a destroy-before-create replacement (not an in-place update).
func TestAcc_Connection_VersionBump(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	connectionID := tg.Name("tfprovider-vbump-conn")

	resource.Test(t, resource.TestCase{
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_11_0),
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.ConnectionCheckDestroy,
		Steps:                    datahubtesting.ConnectionVersionBumpSteps(connectionID),
	})
}

// TestAcc_Connection_NoBlock verifies that omitting the platform block surfaces
// a clear error rather than panicking or creating a malformed connection.
func TestAcc_Connection_NoBlock(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_11_0),
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.ConnectionNoBlockSteps(),
	})
}

// TestAcc_Connection_TwoBlocks verifies that configuring more than one platform
// block at once surfaces the "Multiple platform blocks configured" error.
func TestAcc_Connection_TwoBlocks(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_11_0),
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.ConnectionTwoBlocksSteps(),
	})
}
