// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_LocalUserLogin_WithReset creates a user without an explicit password,
// verifies that password_reset_url is populated, and confirms a second apply
// produces an empty plan (no drift).
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

// TestAcc_LocalUserLogin_Import exercises import by bare username and by URN.
func TestAcc_LocalUserLogin_Import(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	username := tg.Name("tfprovider-login-import")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.LocalUserLoginCheckDestroy,
		Steps:                    datahubtesting.LocalUserLoginImportSteps(username),
	})
}

// TestAcc_LocalUserLogin_Drift verifies that an out-of-band user deletion is
// detected and the login is re-created on the next apply.
func TestAcc_LocalUserLogin_Drift(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	username := tg.Name("tfprovider-login-drift")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.LocalUserLoginCheckDestroy,
		Steps:                    datahubtesting.LocalUserLoginDriftSteps(username),
	})
}

// TestAcc_LocalUserLogin_WithCorpUser tests the two-resource happy path: login
// creates the entity + credentials, then corp_user upserts the profile on top.
func TestAcc_LocalUserLogin_WithCorpUser(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	username := tg.Name("tfprovider-login-two")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.LocalUserLoginCheckDestroy,
		Steps:                    datahubtesting.LocalUserLoginWithCorpUserSteps(username),
	})
}

// TestAcc_LocalUserLogin_CloudUpgrade tests the Cloud upgrade path: create a
// catalog-only user (no credentials) then add a login on top. Succeeds because
// the Cloud signUp guard only rejects users that already have credentials.
func TestAcc_LocalUserLogin_CloudUpgrade(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("Cloud upgrade behavior requires the Cloud fork's signUp guard; live OSS may reject this")
	}
	username := tg.Name("tfprovider-login-cloud")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.LocalUserLoginCheckDestroy,
		Steps:                    datahubtesting.LocalUserLoginCloudUpgradeSteps(username),
	})
}

// TestAcc_LocalUserLogin_OSSRejectsExisting enables OSS signUp mode on the mock
// and verifies that signUp rejects a pre-existing catalog-only user.
func TestAcc_LocalUserLogin_OSSRejectsExisting(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("OSS signUp guard behavior is only testable against the mock with oss-signup-mode enabled")
	}
	username := tg.Name("tfprovider-login-oss")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.LocalUserLoginOSSRejectsExistingSteps(username),
	})
}

// TestAcc_LocalUserLogin_AlreadyHasCredentials verifies that signUp fails when
// the user already has credentials (from a prior signUp). This is the behavior
// on both OSS and Cloud.
func TestAcc_LocalUserLogin_AlreadyHasCredentials(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("requires two signUp calls for the same username; live cleanup between steps is fragile")
	}
	username := tg.Name("tfprovider-login-creds")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.LocalUserLoginAlreadyHasCredentialsSteps(username),
	})
}
