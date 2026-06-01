// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_LocalUserLogin_WithReset creates a user without an explicit password
// and verifies that password_reset_url is populated.
func TestAcc_LocalUserLogin_WithReset(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	username := tg.Name("tfprovider-login-reset")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.LocalUserLoginCheckDestroy,
		Steps:                    datahubtesting.LocalUserLoginWithResetSteps(username),
	})
}

// TestAcc_LocalUserLogin_WithPassword creates a user with an explicit
// initial_password and verifies that password_reset_url is null.
func TestAcc_LocalUserLogin_WithPassword(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	username := tg.Name("tfprovider-login-pw")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.LocalUserLoginCheckDestroy,
		Steps:                    datahubtesting.LocalUserLoginWithPasswordSteps(username),
	})
}

// TestAcc_LocalUserLogin_AlreadyExists verifies that creating a login for an
// already-existing user entity produces the expected error diagnostic.
func TestAcc_LocalUserLogin_AlreadyExists(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("already-exists behavior differs between OSS and Cloud; this test validates the OSS mock behavior")
	}
	username := tg.Name("tfprovider-login-exists")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.LocalUserLoginAlreadyExistsSteps(username),
	})
}
