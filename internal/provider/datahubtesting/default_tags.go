// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// Scenario step builders for provider-level default tags (defaults.tags and
// the computed tags_all ownership latch). The referenced tag is created by a
// datahub_tag resource in an earlier step of the same scenario, satisfying
// the create-before-reference requirement against both mock and live targets.
//
// Destroy-ordering rule (CAT-2701): every scenario's final applied state has
// the entities untagged (defaults removed -> latch released) BEFORE the
// framework's destroy. Destroying the marker tag while entities still carry
// it races DataHub's async deleteReferences cascade, whose stale-graph-scroll
// full-aspect upsert resurrects a concurrently hard-deleted entity as a husk
// and fails CheckDestroy. Remove the unlatch-last steps when CAT-2701 ships.

// tagProviderBlock builds a provider block with defaults.tags set to the
// given tag URN, or a bare provider block when tagURN is empty.
func tagProviderBlock(tagURN string) string {
	if tagURN == "" {
		return "\nprovider \"datahub\" {}\n"
	}
	return fmt.Sprintf("\nprovider \"datahub\" {\n  defaults = {\n    tags = [%q]\n  }\n}\n", tagURN)
}

// tagResourceConfig declares the marker tag used by the scenarios.
func tagResourceConfig(tagID string) string {
	return fmt.Sprintf(`
resource "datahub_tag" "marker" {
  tag_id = %q
  name   = "TF Managed Marker"
}
`, tagID)
}

// CorpGroupDefaultTagsLifecycleSteps covers the full latch lifecycle on
// datahub_corp_group: created unlatched (no defaults, tags_all null), latched
// onto an existing resource when defaults.tags appears, plan idempotency
// while latched, import while latched, and unlatching (defaults removed ->
// aspect cleared, tags_all null again).
func CorpGroupDefaultTagsLifecycleSteps(groupID, tagID string) []resource.TestStep {
	const addr = "datahub_corp_group.test"
	tagURN := "urn:li:tag:" + tagID
	group := fmt.Sprintf(`
resource "datahub_corp_group" "test" {
  group_id = %q
  name     = "Default Tags Group"
}
`, groupID)
	without := tagProviderBlock("") + tagResourceConfig(tagID) + group
	with := tagProviderBlock(tagURN) + tagResourceConfig(tagID) + group

	return []resource.TestStep{
		{
			Config: without,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("tags_all"), knownvalue.Null()),
			},
		},
		{
			// defaults.tags appears: the existing group is latched and tagged.
			Config: with,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("tags_all"), knownvalue.SetExact([]knownvalue.Check{
					knownvalue.StringExact(tagURN),
				})),
			},
		},
		{
			// Idempotency while latched.
			Config:   with,
			PlanOnly: true,
		},
		{
			ResourceName:      addr,
			ImportState:       true,
			ImportStateId:     groupID,
			ImportStateVerify: true,
		},
		{
			// defaults.tags removed: aspect cleared, latch released.
			Config: without,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("tags_all"), knownvalue.Null()),
			},
		},
	}
}

// CorpUserDefaultTagsAtCreateSteps covers tagging at create time on
// datahub_corp_user (the corpuser entity path, shared with
// datahub_service_account).
func CorpUserDefaultTagsAtCreateSteps(username, tagID string) []resource.TestStep {
	const addr = "datahub_corp_user.test"
	tagURN := "urn:li:tag:" + tagID
	cfg := tagProviderBlock(tagURN) + tagResourceConfig(tagID) + fmt.Sprintf(`
resource "datahub_corp_user" "test" {
  username     = %q
  display_name = "Default Tags User"
}
`, username)
	return []resource.TestStep{
		{
			Config: cfg,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("tags_all"), knownvalue.SetExact([]knownvalue.Check{
					knownvalue.StringExact(tagURN),
				})),
			},
		},
		{
			Config:   cfg,
			PlanOnly: true,
		},
	}
}

// DataProductDefaultTagsAtCreateSteps covers tagging at create time on
// datahub_data_product (the dataproduct entity path), coexisting with the
// custom-properties defaults on the same resource.
func DataProductDefaultTagsAtCreateSteps(dataProductID, tagID string) []resource.TestStep {
	const addr = "datahub_data_product.test"
	tagURN := "urn:li:tag:" + tagID
	cfg := tagProviderBlock(tagURN) + tagResourceConfig(tagID) + fmt.Sprintf(`
resource "datahub_data_product" "test" {
  data_product_id = %q
  name            = "Default Tags Product"
}
`, dataProductID)
	return []resource.TestStep{
		{
			Config: cfg,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("tags_all"), knownvalue.SetExact([]knownvalue.Check{
					knownvalue.StringExact(tagURN),
				})),
				// The managed-by marker (on by default) coexists with tags.
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("custom_properties_all"), knownvalue.MapExact(map[string]knownvalue.Check{
					"managed-by": knownvalue.StringExact("terraform"),
				})),
			},
		},
		{
			Config:   cfg,
			PlanOnly: true,
		},
	}
}

// CorpGroupExternalTagSteps covers both sides of the ownership latch against
// external edits (mock-only: the simulated edit writes the raw globalTags
// aspect):
//
//  1. Unlatched (no defaults.tags): a tag applied outside Terraform is
//     invisible - the plan stays empty and the tag is never touched.
//  2. Latched: an externally added tag surfaces as drift on tags_all and is
//     stomped by the next apply.
func CorpGroupExternalTagSteps(groupID, tagID string) []resource.TestStep {
	const addr = "datahub_corp_group.test"
	tagURN := "urn:li:tag:" + tagID
	groupURN := "urn:li:corpGroup:" + groupID
	group := fmt.Sprintf(`
resource "datahub_corp_group" "test" {
  group_id = %q
  name     = "External Tags Group"
}
`, groupID)
	without := tagProviderBlock("") + tagResourceConfig(tagID) + group
	with := tagProviderBlock(tagURN) + tagResourceConfig(tagID) + group

	postExternalTags := func(tags ...string) func() {
		return func() {
			list := make([]string, 0, len(tags))
			for _, t := range tags {
				list = append(list, fmt.Sprintf(`{"tag":%q}`, t))
			}
			url := os.Getenv("DATAHUB_GMS_URL") + "/openapi/v3/entity/corpgroup"
			body := fmt.Sprintf(`[{"urn":%q,"globalTags":{"value":{"tags":[%s]}}}]`, groupURN, strings.Join(list, ","))
			resp, err := http.Post(url, "application/json", strings.NewReader(body)) //nolint:noctx
			if err != nil {
				panic(fmt.Sprintf("CorpGroupExternalTagSteps PreConfig: POST external tags: %v", err))
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode >= 300 {
				panic(fmt.Sprintf("CorpGroupExternalTagSteps PreConfig: unexpected status %d", resp.StatusCode))
			}
		}
	}

	return []resource.TestStep{
		{
			Config: without,
		},
		{
			// Unlatched: an external tag is invisible; the plan is empty.
			PreConfig: postExternalTags("urn:li:tag:ui-applied"),
			Config:    without,
			PlanOnly:  true,
		},
		{
			// Latching now stomps the external tag: the provider owns the
			// full list from here.
			Config: with,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("tags_all"), knownvalue.SetExact([]knownvalue.Check{
					knownvalue.StringExact(tagURN),
				})),
			},
		},
		{
			// While latched, a new external tag surfaces as drift and is
			// removed on apply.
			PreConfig: postExternalTags(tagURN, "urn:li:tag:ui-applied"),
			Config:    with,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("tags_all"), knownvalue.SetExact([]knownvalue.Check{
					knownvalue.StringExact(tagURN),
				})),
			},
		},
	}
}

// CustomAssertionDefaultTagsSteps covers the tags_all latch on
// datahub_custom_assertion (the assertion entity path): tagged at create,
// plan idempotency while latched, and unlatching when defaults.tags is
// removed. Custom assertions work on both OSS and Cloud, so the scenario is
// live-runnable everywhere; entityURN is parameterised so live callers can
// pass an ensured dataset.
func CustomAssertionDefaultTagsSteps(entityURN, tagID string) []resource.TestStep {
	const addr = "datahub_custom_assertion.test"
	tagURN := "urn:li:tag:" + tagID
	assertion := fmt.Sprintf(`
resource "datahub_custom_assertion" "test" {
  entity_urn     = %q
  assertion_type = "CUSTOM"
  description    = "TF Example - default tags assertion"
  platform_urn   = "urn:li:dataPlatform:dbt"
}
`, entityURN)
	with := tagProviderBlock(tagURN) + tagResourceConfig(tagID) + assertion
	without := tagProviderBlock("") + tagResourceConfig(tagID) + assertion

	return []resource.TestStep{
		{
			Config: with,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("tags_all"), knownvalue.SetExact([]knownvalue.Check{
					knownvalue.StringExact(tagURN),
				})),
			},
		},
		{
			Config:   with,
			PlanOnly: true,
		},
		{
			// defaults.tags removed: aspect cleared, latch released.
			Config: without,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("tags_all"), knownvalue.Null()),
			},
		},
	}
}

// typedAssertionDefaultTagsSteps is the shared 3-step shape for tag defaults
// on the typed (monitor-carrying) assertion resources: tagged at create, plan
// idempotency while latched, then unlatch (defaults removed, tags cleared)
// BEFORE the framework's destroy. The unlatch-last ordering is load-bearing
// against live targets, not just extra coverage: see the destroy-ordering
// rule (CAT-2701) in the file header - a pre-fix scenario produced a husk
// live.
func typedAssertionDefaultTagsSteps(addr, assertionConfig, tagID string) []resource.TestStep {
	tagURN := "urn:li:tag:" + tagID
	with := tagProviderBlock(tagURN) + tagResourceConfig(tagID) + assertionConfig
	without := tagProviderBlock("") + tagResourceConfig(tagID) + assertionConfig
	return []resource.TestStep{
		{
			Config: with,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("tags_all"), knownvalue.SetExact([]knownvalue.Check{
					knownvalue.StringExact(tagURN),
				})),
			},
		},
		{
			Config:   with,
			PlanOnly: true,
		},
		{
			// Unlatch before destroy (see doc comment above).
			Config: without,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("tags_all"), knownvalue.Null()),
			},
		},
	}
}

// FreshnessAssertionDefaultTagsAtCreateSteps covers tag defaults on
// datahub_freshness_assertion. Cloud-only on live targets.
func FreshnessAssertionDefaultTagsAtCreateSteps(entityURN, tagID string) []resource.TestStep {
	return typedAssertionDefaultTagsSteps("datahub_freshness_assertion.test", fmt.Sprintf(`
resource "datahub_freshness_assertion" "test" {
  entity_urn              = %q
  schedule_type           = "FIXED_INTERVAL"
  fixed_interval_unit     = "HOUR"
  fixed_interval_multiple = 24
  description             = "TF Example - default tags freshness"
  evaluation_cron         = "0 */8 * * *"
  evaluation_timezone     = "UTC"
  source_type             = "DATAHUB_OPERATION"
  mode                    = "ACTIVE"
}
`, entityURN), tagID)
}

// VolumeAssertionDefaultTagsSteps covers tag defaults on
// datahub_volume_assertion. Cloud-only on live targets.
func VolumeAssertionDefaultTagsSteps(entityURN, tagID string) []resource.TestStep {
	return typedAssertionDefaultTagsSteps("datahub_volume_assertion.test", fmt.Sprintf(`
resource "datahub_volume_assertion" "test" {
  entity_urn          = %q
  volume_type         = "ROW_COUNT_TOTAL"
  operator            = "GREATER_THAN_OR_EQUAL_TO"
  single_value        = "100"
  description         = "TF Example - default tags volume"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  source_type         = "DATAHUB_DATASET_PROFILE"
  mode                = "ACTIVE"
}
`, entityURN), tagID)
}

// SQLAssertionDefaultTagsSteps covers tag defaults on datahub_sql_assertion.
// Cloud-only on live targets.
func SQLAssertionDefaultTagsSteps(entityURN, tagID string) []resource.TestStep {
	return typedAssertionDefaultTagsSteps("datahub_sql_assertion.test", fmt.Sprintf(`
resource "datahub_sql_assertion" "test" {
  entity_urn          = %q
  sql_type            = "METRIC"
  statement           = "SELECT COUNT(*) FROM project.dataset.table WHERE value < 0"
  operator            = "EQUAL_TO"
  value               = "0"
  description         = "TF Example - default tags sql"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  mode                = "ACTIVE"
}
`, entityURN), tagID)
}

// SchemaAssertionDefaultTagsSteps covers tag defaults on
// datahub_schema_assertion. Cloud-only on live targets.
func SchemaAssertionDefaultTagsSteps(entityURN, tagID string) []resource.TestStep {
	return typedAssertionDefaultTagsSteps("datahub_schema_assertion.test", fmt.Sprintf(`
resource "datahub_schema_assertion" "test" {
  entity_urn    = %q
  compatibility = "SUPERSET"
  description   = "TF Example - default tags schema"
  fields = [
    { path = "id", type = "NUMBER", native_type = "INTEGER" },
  ]
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  mode                = "ACTIVE"
}
`, entityURN), tagID)
}

// FieldAssertionDefaultTagsSteps covers tag defaults on
// datahub_field_assertion. Cloud-only on live targets.
func FieldAssertionDefaultTagsSteps(entityURN, tagID string) []resource.TestStep {
	return typedAssertionDefaultTagsSteps("datahub_field_assertion.test", fmt.Sprintf(`
resource "datahub_field_assertion" "test" {
  entity_urn           = %q
  field_assertion_type = "FIELD_METRIC"
  field_path           = "id"
  field_type           = "NUMBER"
  field_native_type    = "INTEGER"
  metric               = "NULL_COUNT"
  operator             = "EQUAL_TO"
  single_value         = "0"
  description          = "TF Example - default tags field"
  failure_severity     = "HIGH"
  source_type          = "DATAHUB_DATASET_PROFILE"
  evaluation_cron      = "0 */8 * * *"
  evaluation_timezone  = "UTC"
  mode                 = "ACTIVE"
}
`, entityURN), tagID)
}

// DefaultTagsNonexistentSteps asserts that referencing a tag that does not
// exist in defaults.tags fails at apply time with a clear error instead of
// silently creating a dangling association.
func DefaultTagsNonexistentSteps(groupID string) []resource.TestStep {
	cfg := tagProviderBlock("urn:li:tag:does-not-exist-"+groupID) + fmt.Sprintf(`
resource "datahub_corp_group" "test" {
  group_id = %q
  name     = "Nonexistent Tag Group"
}
`, groupID)
	return []resource.TestStep{
		{
			Config:      cfg,
			ExpectError: regexp.MustCompile(`does not exist in DataHub`),
		},
	}
}
