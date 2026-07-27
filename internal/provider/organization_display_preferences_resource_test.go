// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_OrganizationDisplayPreferences_Lifecycle covers the singleton
// lifecycle: set both fields, plan idempotency, in-place update of one field
// (the sibling must survive DataHub's per-field merge), import, dropping a
// field, and resetting both.
//
// Live note: these are instance-global settings that cannot be namespaced per
// test run, so a live run mutates real org branding. The scenario resets both
// fields in its final step.
func TestAcc_OrganizationDisplayPreferences_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		tg.RequireCloud(t) // Cloud-only resource; skips on live OSS targets
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.OrganizationDisplayPreferencesLifecycleSteps(),
	})
}

// TestAcc_OrganizationDisplayPreferences_DataSource proves the data source
// reads the same singleton the resource writes.
func TestAcc_OrganizationDisplayPreferences_DataSource(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		tg.RequireCloud(t)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.OrganizationDisplayPreferencesDataSourceSteps(),
	})
}

// TestAcc_OrganizationDisplayPreferences_OSS_RejectsWithCloudOnlyError asserts
// that applying this resource against open-source DataHub fails with the
// provider's Cloud-only diagnostic rather than a raw GraphQL schema error. Runs
// against live OSS targets only (the nightly Quickstart job exercises it).
func TestAcc_OrganizationDisplayPreferences_OSS_RejectsWithCloudOnlyError(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireOSS(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "datahub_organization_display_preferences" "oss_error_test" {
  org_name = "TF Example OSS Error"
}
`,
				ExpectError: regexp.MustCompile(`require DataHub Cloud`),
			},
		},
	})
}

// TestAcc_OrganizationDisplayPreferences_ExternalEdit proves the provider owns
// the values it manages: an edit made outside Terraform surfaces as drift and
// is corrected on the next apply.
func TestAcc_OrganizationDisplayPreferences_ExternalEdit(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("external-edit simulation drives the mutation out-of-band; mock-only")
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.OrganizationDisplayPreferencesExternalEditSteps(),
	})
}
