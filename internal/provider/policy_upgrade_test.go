// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_Policy_UpgradeFromPublished pins backwards compatibility of the
// resources block. Adding `filter` inside an existing SingleNestedAttribute
// changes that attribute's object type, and the provider ships no schema version
// or state upgrader -- so prior state written by a released version has to decode
// against the new schema unchanged.
//
// Step 1 applies a legacy-form policy using the last published provider from the
// registry; step 2 swaps in this build and plans only. An empty plan proves that
// existing configurations neither error on state upgrade nor acquire a spurious
// diff from the new attribute.
func TestAcc_Policy_UpgradeFromPublished(t *testing.T) {
	// A dev_overrides block makes Terraform serve datahub-project/datahub from the
	// local build and ignore required_providers entirely, so step 1 would silently
	// run on the build under test instead of the published one and the test would
	// pass no matter what. `make dev-override` sets this via .mise.env for local
	// development, so refuse to run rather than report a meaningless pass.
	if cfg := os.Getenv("TF_CLI_CONFIG_FILE"); cfg != "" {
		t.Skipf("TF_CLI_CONFIG_FILE=%s is set; a dev override would make this test vacuous. "+
			"Re-run with it unset: mise exec -- env -u TF_CLI_CONFIG_FILE go test ...", cfg)
	}

	tg := datahubtesting.SetupTarget(t)
	if tg.Kind != datahubtesting.TargetMock {
		t.Skip("upgrade test pins schema compatibility, not server behaviour; mock target only")
	}
	policyID := tg.Name("tfprovider-policy-upgrade")

	resource.Test(t, resource.TestCase{
		Steps: datahubtesting.PolicyUpgradeSteps(policyID, testAccProtoV6ProviderFactories),
	})
}
