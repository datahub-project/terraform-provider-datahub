// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// Scenario builders for datahub_oauth_authorization_server.
//
// The scenarios are target-agnostic (mock or live DataHub Cloud). The secret
// rotation steps rely on client_secret_urn changing per rotation, which the
// mock models the way the real server behaves: every non-empty clientSecret
// write mints a NEW dataHubSecret entity.

const oauthServerAddr = "datahub_oauth_authorization_server.test"

// OAuthAuthorizationServerLifecycleSteps covers create, plan idempotency,
// in-place update, import, and clearing optional fields.
func OAuthAuthorizationServerLifecycleSteps(serverID string) []resource.TestStep {
	urn := "urn:li:oauthAuthorizationServer:" + serverID

	fullConfig := func(displayName string) string {
		return providerBlock + fmt.Sprintf(`
resource "datahub_oauth_authorization_server" "test" {
  server_id    = %q
  display_name = %q
  description  = "TF test OAuth server"

  client_id                = "client-abc"
  client_secret_wo         = "s3cret-value"
  client_secret_wo_version = 1

  authorization_url = "https://idp.example.com/oauth/authorize"
  token_url         = "https://idp.example.com/oauth/token"
  scopes            = ["refresh_token", "session:role:READER"]

  additional_token_params = {
    audience = "https://api.example.com"
  }
}
`, serverID, displayName)
	}

	return []resource.TestStep{
		{
			Config: fullConfig("TF Test OAuth Server"),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("id"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("server_id"), knownvalue.StringExact(serverID)),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("display_name"), knownvalue.StringExact("TF Test OAuth Server")),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("has_client_secret"), knownvalue.Bool(true)),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("client_secret_urn"), knownvalue.NotNull()),
				// WriteOnly: never persisted to state.
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("client_secret_wo"), knownvalue.Null()),
				// Schema defaults mirror the server defaults.
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("token_auth_method"), knownvalue.StringExact("POST_BODY")),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("auth_location"), knownvalue.StringExact("HEADER")),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("auth_header_name"), knownvalue.StringExact("Authorization")),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("auth_scheme"), knownvalue.StringExact("Bearer")),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("scopes"), knownvalue.ListExact([]knownvalue.Check{
					knownvalue.StringExact("refresh_token"),
					knownvalue.StringExact("session:role:READER"),
				})),
			},
		},
		{
			// Re-applying the same config must be a no-op.
			Config:   fullConfig("TF Test OAuth Server"),
			PlanOnly: true,
		},
		{
			// In-place update of a non-secret field: the server must be
			// updated, never replaced (replacement would cascade into every
			// referencing AI plugin).
			Config: fullConfig("TF Test OAuth Server Renamed"),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(oauthServerAddr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("display_name"), knownvalue.StringExact("TF Test OAuth Server Renamed")),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("has_client_secret"), knownvalue.Bool(true)),
			},
		},
		{
			// Import by URN. The secret is unreadable by design, and the
			// version lives only in config, so both are ignored.
			ResourceName:            oauthServerAddr,
			ImportState:             true,
			ImportStateId:           urn,
			ImportStateVerify:       true,
			ImportStateVerifyIgnore: []string{"client_secret_wo_version"},
		},
		{
			// Dropping optional fields clears them server-side: the provider
			// owns the complete state of what it declares, so omission is a
			// clear, not a leave-alone. The secret stays: its version is
			// unchanged, so the provider sends null (preserve).
			Config: providerBlock + fmt.Sprintf(`
resource "datahub_oauth_authorization_server" "test" {
  server_id    = %q
  display_name = "TF Test OAuth Server Renamed"

  client_secret_wo_version = 1

  token_url = "https://idp.example.com/oauth/token"
}
`, serverID),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("description"), knownvalue.Null()),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("client_id"), knownvalue.Null()),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("authorization_url"), knownvalue.Null()),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("scopes"), knownvalue.Null()),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("additional_token_params"), knownvalue.Null()),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("has_client_secret"), knownvalue.Bool(true)),
			},
		},
	}
}

// OAuthAuthorizationServerRotationSteps proves the three secret rules that
// distinguish this resource from datahub_secret and datahub_connection:
//
//  1. an unrelated update does NOT touch the secret (same client_secret_urn -
//     re-sending it would mint and orphan a new dataHubSecret per apply);
//  2. a version bump with a value rotates IN PLACE (Update, not replacement,
//     and a NEW client_secret_urn - the old secret entity is orphaned);
//  3. a version bump without a value CLEARS the secret.
func OAuthAuthorizationServerRotationSteps(serverID string) []resource.TestStep {
	config := func(displayName, secretLine string, version int64) string {
		return providerBlock + fmt.Sprintf(`
resource "datahub_oauth_authorization_server" "test" {
  server_id    = %q
  display_name = %q
%s  client_secret_wo_version = %d

  token_url = "https://idp.example.com/oauth/token"
}
`, serverID, displayName, secretLine, version)
	}
	withSecret := func(displayName, secret string, version int64) string {
		return config(displayName, fmt.Sprintf("  client_secret_wo         = %q\n", secret), version)
	}

	// Captured across steps to prove the URN's stability or movement.
	var initialSecretURN string

	return []resource.TestStep{
		{
			Config: withSecret("TF Rotation", "secret-v1", 1),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("has_client_secret"), knownvalue.Bool(true)),
			},
			Check: resource.TestCheckResourceAttrWith(oauthServerAddr, "client_secret_urn", func(v string) error {
				initialSecretURN = v
				return nil
			}),
		},
		{
			// Rule 1: an unrelated update (display name) with an unchanged
			// version preserves the stored secret - the URN must not move.
			Config: withSecret("TF Rotation Renamed", "secret-v1", 1),
			Check: resource.TestCheckResourceAttrWith(oauthServerAddr, "client_secret_urn", func(v string) error {
				if v != initialSecretURN {
					return fmt.Errorf("an unrelated update rotated the secret (URN %s -> %s); the provider must send null when the version is unchanged, or every apply orphans a dataHubSecret", initialSecretURN, v)
				}
				return nil
			}),
		},
		{
			// Rule 2: a version bump with a new value rotates in place. The
			// plan must be an Update - a replacement would cascade into every
			// referencing AI plugin - and the secret URN must move (the
			// previous dataHubSecret entity is orphaned, per the server).
			Config: withSecret("TF Rotation Renamed", "secret-v2", 2),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(oauthServerAddr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("has_client_secret"), knownvalue.Bool(true)),
			},
			Check: resource.TestCheckResourceAttrWith(oauthServerAddr, "client_secret_urn", func(v string) error {
				if v == initialSecretURN {
					return fmt.Errorf("version bump did not rotate the secret (URN still %s)", v)
				}
				return nil
			}),
		},
		{
			// Rule 3: a version bump with the value removed clears the secret.
			Config: config("TF Rotation Renamed", "", 3),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(oauthServerAddr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("has_client_secret"), knownvalue.Bool(false)),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("client_secret_urn"), knownvalue.Null()),
			},
		},
	}
}

// OAuthAuthorizationServerDefaultsSteps proves a minimal config resolves the
// schema defaults (which mirror the server's own) and stays plan-clean.
func OAuthAuthorizationServerDefaultsSteps(serverID string) []resource.TestStep {
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_oauth_authorization_server" "test" {
  server_id    = %q
  display_name = "TF Minimal OAuth Server"
}
`, serverID)

	return []resource.TestStep{
		{
			Config: cfg,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("token_auth_method"), knownvalue.StringExact("POST_BODY")),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("auth_location"), knownvalue.StringExact("HEADER")),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("auth_header_name"), knownvalue.StringExact("Authorization")),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("auth_scheme"), knownvalue.StringExact("Bearer")),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("has_client_secret"), knownvalue.Bool(false)),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("description"), knownvalue.Null()),
				statecheck.ExpectKnownValue(oauthServerAddr, tfjsonpath.New("scopes"), knownvalue.Null()),
			},
		},
		{
			Config:   cfg,
			PlanOnly: true,
		},
	}
}
