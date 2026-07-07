// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_CorpUser_Lifecycle exercises create, in-place update of profile
// fields, and import (by username and by URN) for datahub_corp_user.
func TestAcc_CorpUser_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	username := tg.Name("tfprovider-user")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.CorpUserCheckDestroy,
		Steps:                    datahubtesting.CorpUserLifecycleSteps(username),
	})
}

// TestAcc_CorpUser_Drift verifies that an out-of-band user deletion is detected
// and the user is re-created on the next apply.
// TestAcc_CorpUser_CustomProperties covers custom_properties set at create,
// updated, imported, and cleared, with the display_name/email clobber guard.
func TestAcc_CorpUser_CustomProperties(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	username := tg.Name("tfprovider-user-cp")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.CorpUserCheckDestroy,
		Steps:                    datahubtesting.CorpUserCustomPropertiesSteps(username),
	})
}

// TestAcc_CorpUser_CustomPropertiesValidation asserts invalid custom_properties
// inputs are rejected at plan time by the schema validator.
func TestAcc_CorpUser_CustomPropertiesValidation(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.CorpUserCustomPropertiesValidationSteps(),
	})
}

func TestAcc_CorpUser_Drift(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	username := tg.Name("tfprovider-user-drift")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.CorpUserCheckDestroy,
		Steps:                    datahubtesting.CorpUserDriftSteps(username),
	})
}
