// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// Scenarios for the OSS-1216 write guard: a DataHub policy write deletes any
// actors.roles the policy holds, because the updatePolicy mutation cannot
// carry them and the server rebuilds the aspect from the mutation input.
//
// All of these are mock-only. A role-bearing policy cannot be produced through
// the provider (that is the whole point), so the state has to be seeded
// directly, and the policies on a live instance that hold roles are DataHub's
// own -- pointing a test at those would mean damaging the instance to find out
// whether the guard works.

// SeedRolePolicy injects a policy with role-based actors, and optionally the
// non-editable marker DataHub puts on the policies it ships, straight into the
// mock store. Mock-only: /test-control/seed-role-policy does not exist on a
// live instance.
func SeedRolePolicy(baseURL, policyID, name string, roles []string, editable bool) {
	quoted := make([]string, 0, len(roles))
	for _, r := range roles {
		quoted = append(quoted, fmt.Sprintf("%q", r))
	}
	body := fmt.Sprintf(`{"id":%q,"name":%q,"roles":[%s],"editable":%t}`,
		policyID, name, strings.Join(quoted, ","), editable)
	resp, err := http.Post(baseURL+"/test-control/seed-role-policy", "application/json", strings.NewReader(body)) //nolint:noctx
	if err != nil {
		panic(fmt.Sprintf("SeedRolePolicy: %v", err))
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		panic(fmt.Sprintf("SeedRolePolicy: unexpected status %d", resp.StatusCode))
	}
}

// assertPolicyRoles reads the policy back and fails hard unless it still holds
// exactly the roles given. This is the assertion that matters: the refusal
// diagnostic is only worth anything if the roles survived it, and the mock
// reproduces the strip, so a guard that let the mutation through would leave
// the roles gone here.
func assertPolicyRoles(policyID string, want []string) {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		panic(fmt.Sprintf("assertPolicyRoles: building client: %v", err))
	}
	p, err := client.GetPolicyByURN(context.Background(), "urn:li:dataHubPolicy:"+policyID)
	if err != nil {
		panic(fmt.Sprintf("assertPolicyRoles: reading %s: %v", policyID, err))
	}
	if p == nil {
		panic(fmt.Sprintf("assertPolicyRoles: policy %s is gone", policyID))
	}
	if strings.Join(p.ActorRoles, ",") != strings.Join(want, ",") {
		panic(fmt.Sprintf("assertPolicyRoles: policy %s has roles %v, want %v -- "+
			"the refused write reached DataHub after all", policyID, p.ActorRoles, want))
	}
}

// PolicyRoleGuardCreateSteps covers the case a configuration falls into by
// accident: a datahub_policy naming a policy_id that already exists on the
// server with role-based actors. Create is an upsert at a deterministic URN,
// so nothing about "this resource is new to Terraform" makes the write safe.
func PolicyRoleGuardCreateSteps(policyID string) []resource.TestStep {
	roles := []string{"urn:li:dataHubRole:Admin"}

	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_policy" "test" {
  policy_id  = %q
  name       = "Admins - Platform Policy"
  type       = "PLATFORM"
  privileges = ["MANAGE_POLICIES"]
  actors = {
    users = ["urn:li:corpuser:datahub"]
  }
}
`, policyID)

	return []resource.TestStep{
		{
			PreConfig: func() {
				SeedRolePolicy(os.Getenv("DATAHUB_GMS_URL"), policyID,
					"Admins - Platform Policy", roles, false)
			},
			Config:      cfg,
			ExpectError: regexp.MustCompile(`Policy grants access through roles that this write would delete`),
		},
		{
			// The refusal has to have preserved the roles, not merely
			// complained about them. Nothing else in the suite would notice a
			// guard that reported the hazard and then wrote anyway.
			PreConfig: func() { assertPolicyRoles(policyID, roles) },
			Config:    cfg,
			// Naming the roles is the whole value of the diagnostic: "this
			// policy has roles" would not tell an operator which grant they
			// were about to destroy.
			ExpectError: regexp.MustCompile(`urn:li:dataHubRole:Admin`),
		},
		{
			PreConfig:   func() { assertPolicyRoles(policyID, roles) },
			Config:      cfg,
			ExpectError: regexp.MustCompile(`OSS-1216`),
		},
		{
			// Roles are this policy's only actor binding, so the diagnostic
			// must say so rather than describing a narrowing.
			PreConfig:   func() { assertPolicyRoles(policyID, roles) },
			Config:      cfg,
			ExpectError: regexp.MustCompile(`granting its privileges to nobody`),
		},
	}
}

// PolicyRoleGuardImportSteps walks the path the documentation leads to:
// import a role-bearing policy, then try to apply. The import is allowed --
// reading is harmless, and the warning it emits is the earliest point the
// provider can say anything -- but the apply that follows is refused.
func PolicyRoleGuardImportSteps(policyID string) []resource.TestStep {
	const addr = "datahub_policy.test"
	roles := []string{"urn:li:dataHubRole:Editor"}

	cfg := func(name string) string {
		return providerBlock + fmt.Sprintf(`
resource "datahub_policy" "test" {
  policy_id  = %q
  name       = %q
  type       = "PLATFORM"
  privileges = ["MANAGE_POLICIES"]
  actors     = {}
}
`, policyID, name)
	}

	return []resource.TestStep{
		{
			PreConfig: func() {
				SeedRolePolicy(os.Getenv("DATAHUB_GMS_URL"), policyID,
					"Editors - Edit Metadata Entities", roles, true)
			},
			Config:             cfg("Editors - Edit Metadata Entities"),
			ResourceName:       addr,
			ImportState:        true,
			ImportStateId:      policyID,
			ImportStatePersist: true,
		},
		{
			// Importing must not have written anything.
			PreConfig:   func() { assertPolicyRoles(policyID, roles) },
			Config:      cfg("Editors - Renamed By Terraform"),
			ExpectError: regexp.MustCompile(`Policy grants access through roles that this write would delete`),
		},
		{
			PreConfig:   func() { assertPolicyRoles(policyID, roles) },
			Config:      cfg("Editors - Renamed By Terraform"),
			ExpectError: regexp.MustCompile(`terraform state rm`),
		},
	}
}

// PolicyRoleGuardAllowsRoleFreeSteps is the negative case, and it is not
// optional: a guard that refuses everything is worse than no guard, and this
// one now runs a live read before every create and update.
//
// Two shapes the guard must let through, both of which it sees as an existing
// policy rather than a clean create: a role-free policy already on the server
// (adopted by a create-as-upsert), and one DataHub marks non-editable, which
// warns but must not block.
func PolicyRoleGuardAllowsRoleFreeSteps(existingID, nonEditableID string) []resource.TestStep {
	cfg := func(description string) string {
		return providerBlock + fmt.Sprintf(`
resource "datahub_policy" "adopted" {
  policy_id   = %q
  name        = "Adopted Role-Free Policy"
  type        = "PLATFORM"
  description = %q
  privileges  = ["MANAGE_POLICIES"]
  actors = {
    users = ["urn:li:corpuser:datahub"]
  }
}

resource "datahub_policy" "non_editable" {
  policy_id   = %q
  name        = "Adopted Non-Editable Policy"
  type        = "PLATFORM"
  description = %q
  privileges  = ["MANAGE_POLICIES"]
  actors = {
    all_users = true
  }
}
`, existingID, description, nonEditableID, description)
	}

	return []resource.TestStep{
		{
			PreConfig: func() {
				base := os.Getenv("DATAHUB_GMS_URL")
				// No roles, editable: the ordinary case.
				SeedRolePolicy(base, existingID, "Adopted Role-Free Policy", nil, true)
				// No roles, non-editable: warns, must still apply.
				SeedRolePolicy(base, nonEditableID, "Adopted Non-Editable Policy", nil, false)
			},
			Config: cfg("first"),
		},
		{
			// And the update path, which runs the same guard.
			Config: cfg("second"),
		},
	}
}
