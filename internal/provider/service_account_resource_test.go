// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_ServiceAccount_Lifecycle exercises create, in-place update, and import
// (by bare id and by URN) for datahub_service_account. Requires DataHub Core
// >= 1.4.0 or Cloud; runs against the mock in `make test`.
func TestAcc_ServiceAccount_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	id := tg.Name("tfprovider-sa")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.ServiceAccountCheckDestroy,
		Steps:                    datahubtesting.ServiceAccountLifecycleSteps(id),
	})
}

// TestAcc_ServiceAccount_RefuseNonServiceAccount verifies the resource refuses
// to import a corpUser that lacks the SERVICE_ACCOUNT subtype.
// TestAcc_ServiceAccount_CustomProperties covers custom_properties set at
// create, updated, imported, and cleared, with the display_name/description
// clobber guard.
func TestAcc_ServiceAccount_CustomProperties(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	id := tg.Name("tfprovider-sa-cp")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.ServiceAccountCheckDestroy,
		Steps:                    datahubtesting.ServiceAccountCustomPropertiesSteps(id),
	})
}

// TestAcc_ServiceAccount_CustomPropertiesValidation asserts invalid
// custom_properties inputs are rejected at plan time by the schema validator.
func TestAcc_ServiceAccount_CustomPropertiesValidation(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.ServiceAccountCustomPropertiesValidationSteps(),
	})
}

func TestAcc_ServiceAccount_RefuseNonServiceAccount(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	id := tg.Name("tfprovider-sa-probe")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.ServiceAccountCheckDestroy,
		Steps:                    datahubtesting.ServiceAccountRefuseNonSASteps(id),
	})
}

// TestAcc_ServiceAccount_RoleAssignmentCoexist assigns a role to a service
// account and confirms it is still recognized as a service account afterward
// (subtype and roleMembership coexist).
func TestAcc_ServiceAccount_RoleAssignmentCoexist(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	id := tg.Name("tfprovider-sa-role")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.ServiceAccountCheckDestroy,
		Steps:                    datahubtesting.ServiceAccountRoleAssignmentSteps(id),
	})
}

// TestAcc_ServiceAccount_AsPolicyActor confirms a service account URN is
// accepted as a datahub_policy actor.
func TestAcc_ServiceAccount_AsPolicyActor(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	id := tg.Name("tfprovider-sa-pol")
	policyID := tg.Name("tf-example-sa-policy")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.ServiceAccountCheckDestroy,
		Steps:                    datahubtesting.ServiceAccountAsPolicyActorSteps(id, policyID),
	})
}
