// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"regexp"

	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Scenario builders for the plan-time dataset URN validator. Every scenario is
// PlanOnly: the assertion is about the validate/plan boundary, so nothing is
// written and the mock server is never reached.

const datasetURNErrorPattern = `Invalid dataset URN`

// DatasetURNMalformedDataContractSteps feeds datahub_data_contract a dataset_urn
// with no closing parenthesis and expects the validator's plan-time error.
func DatasetURNMalformedDataContractSteps() []resource.TestStep {
	return []resource.TestStep{{
		Config: providerBlock + `
resource "datahub_data_contract" "test" {
  dataset_urn                 = "urn:li:dataset:(urn:li:dataPlatform:postgres,db.t,PROD"
  data_quality_assertion_urns = ["urn:li:assertion:tf-example-dq"]
}
`,
		PlanOnly:    true,
		ExpectError: regexp.MustCompile(datasetURNErrorPattern),
	}}
}

// DatasetURNMalformedVolumeAssertionSteps feeds datahub_volume_assertion an
// entity_urn that is a bare dataset name and expects the plan-time error.
func DatasetURNMalformedVolumeAssertionSteps() []resource.TestStep {
	return []resource.TestStep{{
		Config: providerBlock + `
resource "datahub_volume_assertion" "test" {
  entity_urn          = "db.schema.table"
  volume_type         = "ROW_COUNT_TOTAL"
  operator            = "GREATER_THAN_OR_EQUAL_TO"
  min_value           = "1"
  evaluation_cron     = "0 * * * *"
  evaluation_timezone = "UTC"
  source_type         = "QUERY"
  mode                = "ACTIVE"
}
`,
		PlanOnly:    true,
		ExpectError: regexp.MustCompile(datasetURNErrorPattern),
	}}
}

// DatasetURNUnknownThenValidSteps proves the validator skips unknown values.
// Step one feeds dataset_urn from terraform_data.seed.output, which is unknown
// during a create plan; it must plan rather than be rejected. Step two feeds a
// well-formed URN through a variable. A literal would not exercise the unknown
// path at all, which is why this is not written with a literal.
func DatasetURNUnknownThenValidSteps() []resource.TestStep {
	const seed = `
resource "terraform_data" "seed" {
  input = "urn:li:dataset:(urn:li:dataPlatform:postgres,db.t,PROD)"
}

resource "datahub_data_contract" "test" {
  dataset_urn                 = terraform_data.seed.output
  data_quality_assertion_urns = ["urn:li:assertion:tf-example-dq"]
}
`
	const viaVariable = `
variable "ds" {
  type = string
}

resource "datahub_data_contract" "test" {
  dataset_urn                 = var.ds
  data_quality_assertion_urns = ["urn:li:assertion:tf-example-dq"]
}
`
	return []resource.TestStep{
		{
			Config:             providerBlock + seed,
			PlanOnly:           true,
			ExpectNonEmptyPlan: true,
		},
		{
			Config: providerBlock + viaVariable,
			ConfigVariables: config.Variables{
				"ds": config.StringVariable("urn:li:dataset:(urn:li:dataPlatform:postgres,db.t,PROD)"),
			},
			PlanOnly:           true,
			ExpectNonEmptyPlan: true,
		},
	}
}
