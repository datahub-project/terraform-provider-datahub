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

// StructuredPropertyAssignmentNewTargetsSteps exercises structured-property
// assignments against the governance entity types added after the original
// four: corpGroup (full lifecycle: create, in-place update, import), corpUser,
// and dataContract (create smoke). One definition targets all three types.
func StructuredPropertyAssignmentNewTargetsSteps(propertyID, groupID, username, contractDatasetURN string) []resource.TestStep {
	const addrGroup = "datahub_structured_property_assignment.group"
	const addrUser = "datahub_structured_property_assignment.user"
	const addrContract = "datahub_structured_property_assignment.contract"
	propURN := "urn:li:structuredProperty:" + propertyID
	groupURN := "urn:li:corpGroup:" + groupID
	importID := groupURN + "|" + propURN

	cfg := func(groupValue string) string {
		return providerBlock + fmt.Sprintf(`
resource "datahub_structured_property" "steward_team" {
  property_id  = %q
  value_type   = "string"
  entity_types = ["corpuser", "corpGroup", "dataContract"]
}

resource "datahub_corp_group" "test" {
  group_id = %q
  name     = "SP New Targets Group"
}

resource "datahub_corp_user" "test" {
  username     = %q
  display_name = "SP New Targets User"
}

resource "datahub_data_contract" "test" {
  dataset_urn                 = %q
  data_quality_assertion_urns = ["urn:li:assertion:tf-sp-targets-dq"]
}

resource "datahub_structured_property_assignment" "group" {
  entity_urn              = datahub_corp_group.test.urn
  structured_property_urn = datahub_structured_property.steward_team.urn
  values                  = [%q]
}

resource "datahub_structured_property_assignment" "user" {
  entity_urn              = datahub_corp_user.test.urn
  structured_property_urn = datahub_structured_property.steward_team.urn
  values                  = ["user-team"]
}

resource "datahub_structured_property_assignment" "contract" {
  entity_urn              = datahub_data_contract.test.urn
  structured_property_urn = datahub_structured_property.steward_team.urn
  values                  = ["contract-team"]
}
`, propertyID, groupID, username, contractDatasetURN, groupValue)
	}

	return []resource.TestStep{
		{
			Config: cfg("group-team"),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addrGroup, tfjsonpath.New("entity_urn"), knownvalue.StringExact(groupURN)),
				statecheck.ExpectKnownValue(addrGroup, tfjsonpath.New("values"), knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("group-team")})),
				statecheck.ExpectKnownValue(addrUser, tfjsonpath.New("values"), knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("user-team")})),
				statecheck.ExpectKnownValue(addrContract, tfjsonpath.New("values"), knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("contract-team")})),
			},
		},
		{
			// In-place value update on the group assignment; the sibling
			// assignments must be no-ops.
			Config: cfg("group-team-2"),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addrGroup, plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction(addrUser, plancheck.ResourceActionNoop),
					plancheck.ExpectResourceAction(addrContract, plancheck.ResourceActionNoop),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addrGroup, tfjsonpath.New("values"), knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("group-team-2")})),
			},
		},
		{
			ResourceName:      addrGroup,
			ImportState:       true,
			ImportStateVerify: true,
			ImportStateId:     importID,
		},
	}
}
