// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Scenario builders for the filter-versus-legacy conflict check when the legacy
// attribute is computed.
//
// DataHub's policy engine returns early when resources.filter is set and ignores
// resources.type / resources.resources / resources.all_resources entirely. A
// configuration setting both is therefore scoped more broadly than it reads,
// which is why ValidateConfig rejects the combination.
//
// The presence tests behind that check read an unresolved value as absent:
//
//	!res.Type.IsNull() && res.Type.ValueString() != ""   // "" for unknown
//	!res.Resources.IsNull() && len(...Elements()) > 0     // empty for unknown
//	!res.AllResources.IsNull() && res.AllResources.ValueBool()  // false for unknown
//
// So the conflict went unreported whenever the legacy attribute came from a
// variable, a module output or another resource's attribute - silently accepting
// an over-permissive policy. The same module errors for a caller passing a
// literal and stays quiet for a caller passing a reference, which is how this
// reaches production.
//
// terraform_data is the built-in provider and needs no credentials; its output is
// unknown during a create plan, which is what makes the referencing expression
// unknown at ValidateResourceConfig time.

// conflictRegexp matches the diagnostic summary all three sites share.
var conflictRegexp = regexp.MustCompile("Conflicting resource scope")

// policyConflictConfig builds a policy setting resources.filter alongside one
// legacy attribute, whose value is supplied by legacyAttr.
func policyConflictConfig(policyID, seedInput, legacyAttr string) string {
	return providerBlock + fmt.Sprintf(`
resource "terraform_data" "seed" {
  input = %q
}

resource "datahub_policy" "test" {
  policy_id  = %q
  name       = "Conflict With Computed Legacy Attribute"
  type       = "METADATA"
  privileges = ["EDIT_ENTITY_TAGS"]
  actors     = { resource_owners = true }

  resources = {
    %s
    filter = { criteria = [{ field = "TAG", values = ["urn:li:tag:pii"] }] }
  }
}
`, seedInput, policyID, legacyAttr)
}

// PolicyConflictComputedTypeSteps sets resources.type from a computed value.
func PolicyConflictComputedTypeSteps(policyID string) []resource.TestStep {
	return []resource.TestStep{{
		Config:      policyConflictConfig(policyID, "dataset", "type = terraform_data.seed.output"),
		PlanOnly:    true,
		ExpectError: conflictRegexp,
	}}
}

// PolicyConflictComputedResourcesSteps sets the whole resources.resources set
// from a computed value, so the set itself is unknown and Elements() is empty.
//
// Note the distinction: [terraform_data.seed.output] is a *known* list of length
// one holding an unknown element, so Elements() sees 1 and the conflict is
// reported correctly even before this fix. Only a wholly unknown collection reads
// as absent. Getting that wrong makes the test pass against broken code.
func PolicyConflictComputedResourcesSteps(policyID string) []resource.TestStep {
	return []resource.TestStep{{
		Config: providerBlock + fmt.Sprintf(`
resource "terraform_data" "seed" {
  input = ["urn:li:dataset:(urn:li:dataPlatform:hive,foo,PROD)"]
}

resource "datahub_policy" "test" {
  policy_id  = %q
  name       = "Conflict With Computed resources"
  type       = "METADATA"
  privileges = ["EDIT_ENTITY_TAGS"]
  actors     = { resource_owners = true }

  resources = {
    resources = terraform_data.seed.output
    filter    = { criteria = [{ field = "TAG", values = ["urn:li:tag:pii"] }] }
  }
}
`, policyID),
		PlanOnly:    true,
		ExpectError: conflictRegexp,
	}}
}

// PolicyConflictComputedAllResourcesSteps sets resources.all_resources from a
// computed value. A Bool reads as false when unknown.
func PolicyConflictComputedAllResourcesSteps(policyID string) []resource.TestStep {
	return []resource.TestStep{{
		Config: providerBlock + fmt.Sprintf(`
resource "terraform_data" "seed" {
  input = true
}

resource "datahub_policy" "test" {
  policy_id  = %q
  name       = "Conflict With Computed all_resources"
  type       = "METADATA"
  privileges = ["EDIT_ENTITY_TAGS"]
  actors     = { resource_owners = true }

  resources = {
    all_resources = terraform_data.seed.output
    filter        = { criteria = [{ field = "TAG", values = ["urn:li:tag:pii"] }] }
  }
}
`, policyID),
		PlanOnly:    true,
		ExpectError: conflictRegexp,
	}}
}

// PolicyLegacyOnlyComputedSteps is the control. The legacy form on its own,
// computed, is entirely legitimate and must not be reported as a conflict - there
// is no filter to conflict with. Guards against a fix that reports the conflict
// whenever the attribute is unknown, regardless of whether a filter is present.
func PolicyLegacyOnlyComputedSteps(policyID string) []resource.TestStep {
	return []resource.TestStep{{
		Config: providerBlock + fmt.Sprintf(`
resource "terraform_data" "seed" {
  input = "dataset"
}

resource "datahub_policy" "test" {
  policy_id  = %q
  name       = "Legacy Form Only"
  type       = "METADATA"
  privileges = ["EDIT_ENTITY_TAGS"]
  actors     = { resource_owners = true }

  resources = {
    type = terraform_data.seed.output
  }
}
`, policyID),
		PlanOnly:           true,
		ExpectNonEmptyPlan: true,
	}}
}
