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
