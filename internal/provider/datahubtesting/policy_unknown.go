// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// PolicyCriteriaFromVariableSteps drives resources.filter.criteria from an
// input variable rather than a literal.
//
// Reported against 0.17.0: the plan fails with
//
//	Received unknown value, however the target type cannot handle unknown
//	values. Path: resources.filter.criteria
//	Target Type: []provider.policyMatchCriterionModel
//
// The variable's element type omits condition, which is Optional+Computed with
// a schema default, so Terraform plans that attribute unknown. A Go slice of
// structs cannot represent unknown, so config conversion fails before the
// provider runs. Literal-only consumption defeats the point of the feature:
// criteria are the natural thing to build in a module or from for_each.
//
// The same defect exists one level up (resources, actors) wherever a Go pointer
// or slice backs a nested attribute containing an Optional+Computed descendant.
func PolicyCriteriaFromVariableSteps(policyID string) []resource.TestStep {
	cfg := providerBlock + `
variable "crit" {
  type = list(object({
    field  = string
    values = list(string)
  }))
}
` + fmt.Sprintf(`
resource "datahub_policy" "test" {
  policy_id  = %q
  name       = "Criteria From Variable"
  type       = "METADATA"
  privileges = ["VIEW_ENTITY_PAGE"]

  actors = {
    resource_owners = true
  }

  resources = {
    filter = {
      criteria = var.crit
    }
  }
}
`, policyID)

	return []resource.TestStep{
		{
			Config: cfg,
			ConfigVariables: config.Variables{
				"crit": config.ListVariable(
					config.ObjectVariable(map[string]config.Variable{
						"field":  config.StringVariable("TYPE"),
						"values": config.ListVariable(config.StringVariable("dataset")),
					}),
				),
			},
		},
	}
}

// PolicyReportedReproSteps is the reporter's config, reproduced as closely as a
// test can. It differs from PolicyCriteriaFromVariableSteps in three ways that
// are worth covering rather than assuming equivalent:
//
//   - the value arrives from the variable's own default rather than from
//     ConfigVariables, so Terraform resolves it internally instead of being
//     handed it;
//   - the criterion targets dataHubIngestionSource rather than dataset.
//
// Three deviations the harness forces, none of which touch the mechanism. The
// reported snippet is abbreviated and not runnable as posted: both name and
// actors are Required, and without them the config fails validation with
// "The argument actors is required" long before conversion is attempted, so the
// snippet cannot exhibit the bug verbatim. Both are added at their minimum. The
// third is policy_id, randomised rather than the literal "repro" so the test is
// safe to run against a shared instance.
func PolicyReportedReproSteps(policyID string) []resource.TestStep {
	cfg := providerBlock + `
variable "crit" {
  type    = list(object({ field = string, values = list(string) }))
  default = [{ field = "TYPE", values = ["dataHubIngestionSource"] }]
}
` + fmt.Sprintf(`
resource "datahub_policy" "x" {
  policy_id  = %q
  name       = "Reported Repro"
  type       = "METADATA"
  privileges = ["VIEW_ENTITY_PAGE"]
  actors     = { all_users = true }
  resources  = { filter = { criteria = var.crit } }
}
`, policyID)

	return []resource.TestStep{{Config: cfg}}
}

// StructuredPropertySettingsFromVariableSteps drives the settings block of
// datahub_structured_property from an input variable.
//
// Same defect as the policy blocks, on an unrelated resource: settings is backed
// by *structuredPropertySettingsModel and every one of its six booleans is
// Optional+Computed with a schema default, so a variable whose object type omits
// any of them plans that attribute unknown and conversion fails. Nothing about
// this is policy-specific; the shape is what matters.
func StructuredPropertySettingsFromVariableSteps(propertyID string) []resource.TestStep {
	cfg := providerBlock + `
variable "settings" {
  type = object({
    is_hidden = bool
  })
}
` + fmt.Sprintf(`
resource "datahub_structured_property" "test" {
  property_id  = %q
  value_type   = "string"
  entity_types = ["domain"]

  settings = var.settings
}
`, propertyID)

	return []resource.TestStep{
		{
			Config: cfg,
			ConfigVariables: config.Variables{
				"settings": config.ObjectVariable(map[string]config.Variable{
					"is_hidden": config.BoolVariable(true),
				}),
			},
		},
	}
}

// PolicyActorsFromVariableSteps drives the actors block from an input variable.
//
// This exercises the same defect on a block that predates the criteria feature:
// actors is backed by *policyActorsModel and contains all_users, all_groups and
// resource_owners, all Optional+Computed with defaults. If this fails too, the
// bug is not specific to resources.filter and has been reachable since actors
// shipped, which changes both the fix scope and the workaround advice.
func PolicyActorsFromVariableSteps(policyID string) []resource.TestStep {
	cfg := providerBlock + `
variable "act" {
  type = object({
    users = list(string)
  })
}
` + fmt.Sprintf(`
resource "datahub_policy" "test" {
  policy_id  = %q
  name       = "Actors From Variable"
  type       = "METADATA"
  privileges = ["VIEW_ENTITY_PAGE"]

  actors = var.act

  resources = {
    type = "dataset"
  }
}
`, policyID)

	return []resource.TestStep{
		{
			Config: cfg,
			ConfigVariables: config.Variables{
				"act": config.ObjectVariable(map[string]config.Variable{
					"users": config.ListVariable(
						config.StringVariable("urn:li:corpuser:datahub"),
					),
				}),
			},
		},
	}
}
