// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_UnmanagedAspectFieldsSurviveUpdate seeds a field each resource does
// not manage on the aspect it writes, applies a change that rewrites that
// aspect, and checks the field is still there. Mock-only: the mock gives the
// seeded field DataHub's whole-aspect replace semantics, which is what makes a
// write that drops it visible here.
func TestAcc_UnmanagedAspectFieldsSurviveUpdate(t *testing.T) {
	for i, c := range datahubtesting.UnmanagedFieldCases("") {
		t.Run(c.Name, func(t *testing.T) {
			tg := datahubtesting.SetupTarget(t)
			if tg.Kind != datahubtesting.TargetMock {
				t.Skip("seeding an unmanaged aspect field requires the mock target")
			}
			scoped := datahubtesting.UnmanagedFieldCases(tg.Name("tfprovider-unmanaged"))[i]
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps:                    datahubtesting.UnmanagedFieldSurvivesUpdateSteps(scoped),
			})
		})
	}
}
