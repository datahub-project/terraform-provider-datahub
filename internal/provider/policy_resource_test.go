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

// TestAcc_Policy_RoleGuardCreate verifies the provider refuses to overwrite a
// policy whose actors are bound through DataHub roles, and that the refusal
// leaves those roles intact (OSS-1216). Mock-only: a role-bearing policy cannot
// be created through the provider, and the ones that exist on a live instance
// are DataHub's own.
func TestAcc_Policy_RoleGuardCreate(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.Kind != datahubtesting.TargetMock {
		t.Skip("seeding a role-bearing policy requires the mock target")
	}
	policyID := tg.Name("tfprovider-policy-roles")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.PolicyCheckDestroy,
		Steps:                    datahubtesting.PolicyRoleGuardCreateSteps(policyID),
	})
}

// TestAcc_Policy_RoleGuardImport covers the documented bulk-import path: a
// role-bearing policy imports (with a warning), and the first apply after it is
// refused rather than silently destroying the role bindings.
func TestAcc_Policy_RoleGuardImport(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.Kind != datahubtesting.TargetMock {
		t.Skip("seeding a role-bearing policy requires the mock target")
	}
	policyID := tg.Name("tfprovider-policy-roles-import")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.PolicyCheckDestroy,
		Steps:                    datahubtesting.PolicyRoleGuardImportSteps(policyID),
	})
}

// TestAcc_Policy_RoleGuardAllowsRoleFree proves the guard is discriminating:
// an existing role-free policy is still adopted and updated, and a non-editable
// one warns without blocking.
func TestAcc_Policy_RoleGuardAllowsRoleFree(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.Kind != datahubtesting.TargetMock {
		t.Skip("seeding a pre-existing policy requires the mock target")
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.PolicyCheckDestroy,
		Steps: datahubtesting.PolicyRoleGuardAllowsRoleFreeSteps(
			tg.Name("tfprovider-policy-adopt"),
			tg.Name("tfprovider-policy-noedit"),
		),
	})
}

// TestAcc_Policies_List verifies a created policy's URN appears in the
// datahub_policies enumeration data source. This is the data source's only
// executable coverage against the mock: its handler (handleListPolicies)
// existed with nothing exercising it, so a defect in the Read path would have
// been caught by nobody. Live coverage comes from examples/runnable/local-iam,
// which reads the same data source through the live-example harness.
func TestAcc_Policies_List(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("list data source is OpenSearch-backed and eventually consistent; a just-created policy may not be indexed at read time")
	}
	policyID := tg.Name("tfprovider-policies-list")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.PolicyCheckDestroy,
		Steps:                    datahubtesting.PoliciesListSteps(policyID),
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
