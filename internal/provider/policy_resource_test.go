// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_Policy_Lifecycle exercises create, in-place privilege/description
// update, and import (by id and URN) for a PLATFORM policy.
func TestAcc_Policy_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	policyID := tg.Name("tfprovider-policy")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.PolicyCheckDestroy,
		Steps:                    datahubtesting.PolicyLifecycleSteps(policyID),
	})
}

// TestAcc_Policy_Metadata exercises a METADATA policy with a resources scope.
func TestAcc_Policy_Metadata(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	policyID := tg.Name("tfprovider-policy-meta")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.PolicyCheckDestroy,
		Steps:                    datahubtesting.PolicyMetadataSteps(policyID),
	})
}

// TestAcc_Policy_Filter exercises the criteria-based resources.filter scope:
// multi-type plus tag scoping, an in-place criteria edit, and import fidelity.
func TestAcc_Policy_Filter(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	policyID := tg.Name("tfprovider-policy-filter")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.PolicyCheckDestroy,
		Steps:                    datahubtesting.PolicyFilterSteps(policyID),
	})
}

// TestAcc_Policy_FilterImport imports a filter-scoped policy the provider did not
// create -- the UI-authored case whose scope the read path used to discard.
// Mock-only: it seeds the policy via /test-control/seed-policy.
func TestAcc_Policy_FilterImport(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.Kind != datahubtesting.TargetMock {
		t.Skip("seeding a UI-shaped policy requires the mock target")
	}
	policyID := tg.Name("tfprovider-policy-seeded")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.PolicyCheckDestroy,
		Steps:                    datahubtesting.PolicyFilterImportSteps(policyID),
	})
}

// TestAcc_Policy_FilterConflicts verifies the plan-time guards that stop a
// filter being silently overridden by the deprecated resource attributes.
func TestAcc_Policy_FilterConflicts(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	policyID := tg.Name("tfprovider-policy-conflict")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.PolicyCheckDestroy,
		Steps:                    datahubtesting.PolicyFilterConflictSteps(policyID),
	})
}

// TestAcc_Policy_Drift verifies out-of-band deletion is detected and re-created.
func TestAcc_Policy_Drift(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	policyID := tg.Name("tfprovider-policy-drift")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.PolicyCheckDestroy,
		Steps:                    datahubtesting.PolicyDriftSteps(policyID),
	})
}
