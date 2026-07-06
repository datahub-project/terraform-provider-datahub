// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

func TestAssertionAssignmentRuleResource_lifecycle_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.AssignmentRuleCheckDestroy,
		Steps:                    datahubtesting.AssignmentRuleLifecycleSteps(),
	})
}

// TestAssertionAssignmentRulesDataSource_mock exercises the plural data source
// (Configure + Read) against the mock server: create one rule, then confirm the
// data source enumerates its URN.
func TestAssertionAssignmentRulesDataSource_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	const dsAddr = "data.datahub_assertion_assignment_rules.all"
	const ruleURN = "urn:li:assertionAssignmentRule:tf-ds-test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "datahub_assertion_assignment_rule" "test" {
  rule_id = "tf-ds-test"
  name    = "TF DS Test"
  or_filters = [
    { and = [ { field = "platform", values = ["urn:li:dataPlatform:postgres"] } ] }
  ]
  freshness = { source_type = "INFORMATION_SCHEMA" }
}

data "datahub_assertion_assignment_rules" "all" {
  depends_on = [datahub_assertion_assignment_rule.test]
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(dsAddr, tfjsonpath.New("urns"),
						knownvalue.ListExact([]knownvalue.Check{knownvalue.StringExact(ruleURN)})),
				},
			},
		},
	})
}

func TestAcc_AssertionAssignmentRule_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireCloud(t) // Cloud-only resource; skips on live OSS targets

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.AssignmentRuleCheckDestroy,
		Steps:                    datahubtesting.AssignmentRuleLifecycleSteps(),
	})
}

// TestAcc_AssertionAssignmentRule_OSS_RejectsWithCloudOnlyError verifies that
// datahub_assertion_assignment_rule surfaces a "DataHub Cloud Required"
// diagnostic when applied against an OSS DataHub instance.
func TestAcc_AssertionAssignmentRule_OSS_RejectsWithCloudOnlyError(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireOSS(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "datahub_assertion_assignment_rule" "oss_error_test" {
  rule_id = "tf-example-oss-error"
  name    = "TF Example - OSS Error"
  or_filters = [
    {
      and = [
        { field = "platform", values = ["urn:li:dataPlatform:postgres"] }
      ]
    }
  ]
  freshness = {
    source_type = "INFORMATION_SCHEMA"
  }
}
`,
				ExpectError: regexp.MustCompile(`DataHub Cloud Required`),
			},
		},
	})
}
