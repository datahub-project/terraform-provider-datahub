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
func TestAcc_ServiceAccount_RefuseNonServiceAccount(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	id := tg.Name("tfprovider-sa-probe")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.ServiceAccountCheckDestroy,
		Steps:                    datahubtesting.ServiceAccountRefuseNonSASteps(id),
	})
}
