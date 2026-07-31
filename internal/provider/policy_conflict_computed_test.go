// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// resources.filter combined with a legacy attribute must be rejected whether or
// not the legacy value has resolved. DataHub ignores the legacy attributes when a
// filter is set, so accepting the combination ships a policy scoped more broadly
// than it reads. See the scenario builders for why an unresolved value was read
// as absent.

func TestAcc_Policy_ConflictComputedType(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	policyID := tg.Name("tfprovider-pol-cflt-type")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.PolicyConflictComputedTypeSteps(policyID),
	})
}

func TestAcc_Policy_ConflictComputedResources(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	policyID := tg.Name("tfprovider-pol-cflt-res")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.PolicyConflictComputedResourcesSteps(policyID),
	})
}

func TestAcc_Policy_ConflictComputedAllResources(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	policyID := tg.Name("tfprovider-pol-cflt-all")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.PolicyConflictComputedAllResourcesSteps(policyID),
	})
}

// Control: the legacy form on its own, computed, is legitimate and must plan
// cleanly. Passes before and after the fix; fails only if the fix over-reaches
// and reports a conflict with no filter present.
func TestAcc_Policy_LegacyOnlyComputed_Control(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	policyID := tg.Name("tfprovider-pol-legacy-only")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.PolicyLegacyOnlyComputedSteps(policyID),
	})
}
