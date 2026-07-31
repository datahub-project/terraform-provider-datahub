// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"regexp"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Scenario builders for conditionally-required attributes supplied as computed
// values.
//
// datahub_freshness_assertion and datahub_field_assertion decide which of their
// attributes are required by reading a discriminator (schedule_type,
// field_assertion_type) and then testing the dependent attributes for presence
// with
//
//	has := !cfg.X.IsNull() && cfg.X.ValueString() != ""
//
// On an unknown value IsNull() is false but ValueString() returns "", so a
// set-but-not-yet-resolved attribute reads as absent and the resource rejects
// the configuration with "Missing <attr>" - pointing at the very line that
// supplies the value. datahub_volume_assertion and datahub_sql_assertion guard
// IsUnknown() up front and are unaffected, which is why nothing looked wrong
// next to its neighbours.
//
// Reaching unknown from a test needs a reference to another resource's computed
// attribute: the plugin-testing harness always supplies ConfigVariables values,
// so a variable alone is not enough here. terraform_data is the built-in
// provider and needs no credentials, and its output is unknown during a create
// plan.
//
// Each scenario is PlanOnly with ExpectNonEmptyPlan: the assertion under test is
// that the configuration is accepted and produces a plan, not that anything is
// written. That keeps the scenario about the validate/plan boundary and off the
// server entirely.

const unknownSeedBlock = `
provider "datahub" {}

resource "terraform_data" "seed" {
  input = "DAY"
}
`

const unknownCronSeedBlock = `
provider "datahub" {}

resource "terraform_data" "seed" {
  input = "0 * * * *"
}
`

func planOnlyStep(cfg string) []resource.TestStep {
	return []resource.TestStep{{
		Config:             cfg,
		PlanOnly:           true,
		ExpectNonEmptyPlan: true,
	}}
}

// FreshnessUnknownFixedIntervalSteps supplies fixed_interval_unit as a computed
// value while schedule_type selects FIXED_INTERVAL.
func FreshnessUnknownFixedIntervalSteps() []resource.TestStep {
	return planOnlyStep(unknownSeedBlock + `
resource "datahub_freshness_assertion" "test" {
  entity_urn              = "urn:li:dataset:(urn:li:dataPlatform:snowflake,db.t,PROD)"
  schedule_type           = "FIXED_INTERVAL"
  fixed_interval_unit     = terraform_data.seed.output
  fixed_interval_multiple = 1
  evaluation_cron         = "0 * * * *"
  evaluation_timezone     = "UTC"
  source_type             = "DATAHUB_OPERATION"
  mode                    = "ACTIVE"
}
`)
}

// FreshnessUnknownCronSteps supplies both cron attributes as computed values
// while schedule_type selects CRON.
func FreshnessUnknownCronSteps() []resource.TestStep {
	return planOnlyStep(unknownCronSeedBlock + `
resource "datahub_freshness_assertion" "test" {
  entity_urn          = "urn:li:dataset:(urn:li:dataPlatform:snowflake,db.t,PROD)"
  schedule_type       = "CRON"
  cron_schedule       = terraform_data.seed.output
  cron_timezone       = terraform_data.seed.output
  evaluation_cron     = "0 * * * *"
  evaluation_timezone = "UTC"
  source_type         = "DATAHUB_OPERATION"
  mode                = "ACTIVE"
}
`)
}

// FieldUnknownMetricSteps supplies metric as a computed value while
// field_assertion_type selects FIELD_METRIC.
func FieldUnknownMetricSteps() []resource.TestStep {
	return planOnlyStep(`
provider "datahub" {}

resource "terraform_data" "seed" {
  input = "NULL_COUNT"
}
` + `
resource "datahub_field_assertion" "test" {
  entity_urn            = "urn:li:dataset:(urn:li:dataPlatform:snowflake,db.t,PROD)"
  field_assertion_type  = "FIELD_METRIC"
  field_path            = "id"
  field_type            = "NUMBER"
  metric                = terraform_data.seed.output
  operator              = "EQUAL_TO"
  single_value          = "0"
  evaluation_cron       = "0 * * * *"
  evaluation_timezone   = "UTC"
  source_type           = "ALL_ROWS_QUERY"
  mode                  = "ACTIVE"
}
`)
}

// VolumeUnknownChangeTypeControl is the control: datahub_volume_assertion
// already guards IsUnknown(), so the same shape must plan cleanly. If this ever
// fails, the guard has been lost rather than the other resources being fixed.
func VolumeUnknownChangeTypeControl() []resource.TestStep {
	return planOnlyStep(`
provider "datahub" {}

resource "terraform_data" "seed" {
  input = "ROW_COUNT_TOTAL"
}
` + `
resource "datahub_volume_assertion" "test" {
  entity_urn          = "urn:li:dataset:(urn:li:dataPlatform:snowflake,db.t,PROD)"
  volume_type         = terraform_data.seed.output
  operator            = "LESS_THAN_OR_EQUAL_TO"
  single_value        = "1000"
  evaluation_cron     = "0 * * * *"
  evaluation_timezone = "UTC"
  source_type         = "INFORMATION_SCHEMA"
  mode                = "ACTIVE"
}
`)
}

// FreshnessUnknownWrongBranchSteps supplies fixed_interval_unit as a computed
// value while schedule_type selects CRON, where that attribute does not belong.
//
// The "only valid when" direction of the same presence check. An unknown must
// still be reported as unexpected: the configuration sets the attribute in a
// place it does not belong, and whether the value has resolved is irrelevant to
// that. Getting this direction wrong would silently accept a misconfiguration -
// the opposite failure from the one being fixed, and easy to introduce while
// fixing it.
func FreshnessUnknownWrongBranchSteps() []resource.TestStep {
	return []resource.TestStep{{
		Config: unknownSeedBlock + `
resource "datahub_freshness_assertion" "test" {
  entity_urn          = "urn:li:dataset:(urn:li:dataPlatform:snowflake,db.t,PROD)"
  schedule_type       = "CRON"
  cron_schedule       = "0 * * * *"
  cron_timezone       = "UTC"
  fixed_interval_unit = terraform_data.seed.output
  evaluation_cron     = "0 * * * *"
  evaluation_timezone = "UTC"
  source_type         = "DATAHUB_OPERATION"
  mode                = "ACTIVE"
}
`,
		PlanOnly:    true,
		ExpectError: regexp.MustCompile(`fixed_interval_unit is only valid when schedule_type`),
	}}
}
