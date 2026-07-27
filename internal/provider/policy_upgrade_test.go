// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
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
	// Required: without this a local dev_overrides block serves both steps from the
	// build under test, and the test passes regardless of what the schema does.
	datahubtesting.NeutralizeDevOverride(t)

	tg := datahubtesting.SetupTarget(t)
	if tg.Kind != datahubtesting.TargetMock {
		t.Skip("upgrade test pins schema compatibility, not server behaviour; mock target only")
	}
	policyID := tg.Name("tfprovider-policy-upgrade")

	resource.Test(t, resource.TestCase{
		Steps: datahubtesting.PolicyUpgradeSteps(policyID, testAccProtoV6ProviderFactories),
	})
}
