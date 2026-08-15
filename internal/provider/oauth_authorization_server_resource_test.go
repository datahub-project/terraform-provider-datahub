// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_OAuthAuthorizationServer_Lifecycle covers create, plan idempotency,
// in-place update, import, and clearing optional fields.
func TestAcc_OAuthAuthorizationServer_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		tg.RequireCloud(t) // Cloud-only resource; skips on live OSS targets
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.OAuthAuthorizationServerLifecycleSteps(tg.Name("tf-acc-oauth-lifecycle")),
	})
}

// TestAcc_OAuthAuthorizationServer_SecretRotation proves the three secret
// rules: unrelated updates preserve the stored secret (no orphaned
// dataHubSecret per apply), a version bump with a value rotates IN PLACE
// (update, not the replacement datahub_secret users expect - replacement here
// would cascade into referencing AI plugins), and a version bump without a
// value clears the secret.
func TestAcc_OAuthAuthorizationServer_SecretRotation(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		tg.RequireCloud(t)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.OAuthAuthorizationServerRotationSteps(tg.Name("tf-acc-oauth-rotation")),
	})
}

// TestAcc_OAuthAuthorizationServer_CascadeDeletion proves Read tolerates the
// server being hard-deleted underneath Terraform (deleteAiPlugin's cascade
// removes an unshared server when the last referencing plugin goes): refresh
// must drop it from state and the next apply must be a clean re-create, not a
// wedged plan or a silent no-op against stale state.
func TestAcc_OAuthAuthorizationServer_CascadeDeletion(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		tg.RequireCloud(t)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.OAuthAuthorizationServerCascadeDeletionSteps(tg.Name("tf-acc-oauth-cascade")),
	})
}

// TestAcc_OAuthAuthorizationServer_Defaults proves a minimal config resolves
// the schema defaults (mirroring the server's own: POST_BODY, HEADER,
// Authorization, Bearer) and stays plan-clean.
func TestAcc_OAuthAuthorizationServer_Defaults(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		tg.RequireCloud(t)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.OAuthAuthorizationServerDefaultsSteps(tg.Name("tf-acc-oauth-defaults")),
	})
}

// TestAcc_OAuthAuthorizationServer_InvalidID asserts the plan-time id
// validation: URN-reserved characters and full-URN input fail before any API
// call.
func TestAcc_OAuthAuthorizationServer_InvalidID(t *testing.T) {
	datahubtesting.SetupTarget(t)

	cases := []struct {
		name     string
		serverID string
		want     string
	}{
		{"reserved comma", "bad,id", `URN-reserved character`},
		{"reserved paren", "bad(id", `URN-reserved character`},
		{"full urn", "urn:li:oauthAuthorizationServer:x", `bare id, not a full URN`},
		{"whitespace", " padded ", `leading or trailing whitespace`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: `
provider "datahub" {}

resource "datahub_oauth_authorization_server" "bad" {
  server_id    = "` + tc.serverID + `"
  display_name = "Bad"
}
`,
						ExpectError: regexp.MustCompile(tc.want),
					},
				},
			})
		})
	}
}

// TestAcc_OAuthAuthorizationServer_OSS_RejectsWithCloudOnlyError asserts that
// applying this resource against open-source DataHub fails with the provider's
// Cloud-only diagnostic rather than a raw GraphQL schema error. Runs against
// live OSS targets only (the nightly Quickstart job exercises it).
func TestAcc_OAuthAuthorizationServer_OSS_RejectsWithCloudOnlyError(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireOSS(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "datahub_oauth_authorization_server" "oss_error_test" {
  server_id    = "tf-acc-oauth-oss-error"
  display_name = "TF OSS Error Test"
}
`,
				ExpectError: regexp.MustCompile(`requires DataHub Cloud`),
			},
		},
	})
}
