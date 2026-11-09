// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// MonitorBackedAssertionCase is one of the five monitor-backed assertion
// resources, with two configurations that differ only in the description, so
// moving between them is an in-place update.
type MonitorBackedAssertionCase struct {
	Name string
	Addr string
	// Resource is the resource block alone; Config and Updated add the
	// provider block.
	Resource     string
	Config       string
	Updated      string
	CheckDestroy resource.TestCheckFunc
}

// MonitorBackedAssertionCases returns every monitor-backed assertion resource.
// Create and Update are separate code in each resource, so the monitor link
// lag scenarios run against all five rather than one representative.
func MonitorBackedAssertionCases() []MonitorBackedAssertionCase {
	const sqlite = "urn:li:dataset:(urn:li:dataPlatform:sqlite,tf_assertion_test.tf_test_data,PROD)"
	bodies := []struct {
		name, typ, body string
		destroy         resource.TestCheckFunc
	}{
		{"volume", "datahub_volume_assertion", `
  entity_urn          = "` + sqlite + `"
  volume_type         = "ROW_COUNT_TOTAL"
  operator            = "GREATER_THAN_OR_EQUAL_TO"
  single_value        = "100"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  source_type         = "DATAHUB_DATASET_PROFILE"
  mode                = "ACTIVE"`, VolumeAssertionCheckDestroy},
		{"freshness", "datahub_freshness_assertion", `
  entity_urn          = "urn:li:dataset:(urn:li:dataPlatform:hive,freshness.table,PROD)"
  schedule_type       = "SINCE_THE_LAST_CHECK"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  source_type         = "DATAHUB_OPERATION"
  mode                = "ACTIVE"`, FreshnessAssertionCheckDestroy},
		{"sql", "datahub_sql_assertion", `
  entity_urn          = "urn:li:dataset:(urn:li:dataPlatform:bigquery,project.dataset.table,PROD)"
  sql_type            = "METRIC"
  statement           = "SELECT COUNT(*) FROM project.dataset.table WHERE value < 0"
  operator            = "EQUAL_TO"
  value               = "0"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  mode                = "ACTIVE"`, SQLAssertionCheckDestroy},
		{"field", "datahub_field_assertion", `
  entity_urn           = "` + sqlite + `"
  field_assertion_type = "FIELD_METRIC"
  field_path           = "id"
  field_type           = "NUMBER"
  field_native_type    = "INTEGER"
  metric               = "NULL_COUNT"
  operator             = "EQUAL_TO"
  single_value         = "0"
  source_type          = "DATAHUB_DATASET_PROFILE"
  evaluation_cron      = "0 */8 * * *"
  evaluation_timezone  = "UTC"
  mode                 = "ACTIVE"`, FieldAssertionCheckDestroy},
		{"schema", "datahub_schema_assertion", `
  entity_urn          = "` + sqlite + `"
  compatibility       = "SUPERSET"
  fields = [
    { path = "id", type = "NUMBER", native_type = "INTEGER" },
  ]
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  mode                = "ACTIVE"`, SchemaAssertionCheckDestroy},
	}

	cases := make([]MonitorBackedAssertionCase, 0, len(bodies))
	for _, b := range bodies {
		render := func(description string) string {
			return fmt.Sprintf("resource %q \"test\" {%s\n  description         = %q\n}\n", b.typ, b.body, description)
		}
		cases = append(cases, MonitorBackedAssertionCase{
			Name:         b.name,
			Addr:         b.typ + ".test",
			Resource:     render("monitor link lag"),
			Config:       providerBlock + render("monitor link lag"),
			Updated:      providerBlock + render("monitor link lag, updated"),
			CheckDestroy: b.destroy,
		})
	}
	return cases
}

// mockControl sends one request to a mock test-control endpoint and panics on
// anything but 204, since a control that did not take effect would let the
// scenario pass without exercising what it claims to.
func mockControl(method, pathAndQuery string) {
	req, err := http.NewRequest(method, os.Getenv("DATAHUB_GMS_URL")+pathAndQuery, bytes.NewReader(nil)) //nolint:noctx
	if err != nil {
		panic(fmt.Sprintf("mock control %s %s: %v", method, pathAndQuery, err))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(fmt.Sprintf("mock control %s %s: %v", method, pathAndQuery, err))
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		panic(fmt.Sprintf("mock control %s %s: unexpected status %d", method, pathAndQuery, resp.StatusCode))
	}
}

// AssertionMonitorLinkLagCreateSteps covers a create whose monitor link is not
// indexed by the end of the provider's wait. The caller must shorten the wait
// with datahub.SetMonitorLinkBackoffForTesting; with the default budget the
// first step would take three minutes.
//
//  1. With every link hidden, the apply succeeds (with a warning) and records
//     the assertion with monitor_urn null, rather than failing and leaving the
//     assertion and monitor behind untracked. The framework's post-apply plan
//     proves the null is stable: Read still sees no link and keeps it.
//  2. Once the link is visible, a refresh fills monitor_urn from Read.
//  3. A plan against the same configuration is empty: the refreshed value is
//     neither a diff nor a replacement.
//
// The final destroy then runs with the refreshed monitor_urn, and the caller's
// CheckDestroy proves the monitor went with the assertion.
func AssertionMonitorLinkLagCreateSteps(c MonitorBackedAssertionCase) []resource.TestStep {
	return []resource.TestStep{
		{
			PreConfig: func() { mockControl(http.MethodPost, "/test-control/hide-monitor-links") },
			Config:    c.Config,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(c.Addr, tfjsonpath.New("urn"), knownvalue.StringRegexp(regexp.MustCompile(`^urn:li:assertion:`))),
				statecheck.ExpectKnownValue(c.Addr, tfjsonpath.New("monitor_urn"), knownvalue.Null()),
			},
		},
		{
			PreConfig:    func() { mockControl(http.MethodDelete, "/test-control/hide-monitor-links") },
			RefreshState: true,
			Check:        resource.TestMatchResourceAttr(c.Addr, "monitor_urn", regexp.MustCompile(`^urn:li:monitor:`)),
		},
		{
			Config: c.Config,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{monitorURNCheck(c.Addr)},
		},
	}
}

// AssertionMonitorLinkLagUpdateSteps covers updates refused because the server
// cannot yet resolve the assertion's monitor.
//
//  1. Create normally.
//  2. The next two updates are refused with the server's "Monitor for
//     assertion ... does not exist" error while the provider's own lookup sees
//     the link. The update must resend and succeed, not fail the apply.
//  3. With every link hidden the refusal never clears. The update must give up
//     at the end of the budget with an error saying why and that a retry will
//     work, and leave state as it was.
//
// The final destroy runs with links still hidden and succeeds, because it uses
// the monitor_urn recorded at create rather than the lookup.
func AssertionMonitorLinkLagUpdateSteps(c MonitorBackedAssertionCase) []resource.TestStep {
	return []resource.TestStep{
		{
			Config:            c.Config,
			ConfigStateChecks: []statecheck.StateCheck{monitorURNCheck(c.Addr)},
		},
		{
			PreConfig: func() { mockControl(http.MethodPost, "/test-control/fail-monitor-updates?count=2") },
			Config:    c.Updated,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(c.Addr, tfjsonpath.New("description"), knownvalue.StringExact("monitor link lag, updated")),
				monitorURNCheck(c.Addr),
			},
		},
		{
			PreConfig:   func() { mockControl(http.MethodPost, "/test-control/hide-monitor-links") },
			Config:      c.Config,
			ExpectError: regexp.MustCompile(`has not yet indexed the link`),
		},
	}
}

// AssertionMonitorLinkLagDestroySteps covers destroying an assertion whose
// monitor_urn was never recorded while the link is still hidden. The destroy
// must go ahead -- blocking it would strand the resource over a lag that
// clears by itself -- and warn that the monitor may remain. The last check
// pins what the warning says: the monitor really is still there, because
// nothing could identify it.
//
// Delete is shared by all five resources (deleteAssertionAndMonitor), so one
// resource covers it. The caller must shorten the wait as for the create
// scenario.
func AssertionMonitorLinkLagDestroySteps(c MonitorBackedAssertionCase) []resource.TestStep {
	var assertionURN string
	return []resource.TestStep{
		{
			PreConfig: func() { mockControl(http.MethodPost, "/test-control/hide-monitor-links") },
			Config:    c.Config,
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckNoResourceAttr(c.Addr, "monitor_urn"),
				func(s *terraform.State) error {
					assertionURN = s.RootModule().Resources[c.Addr].Primary.Attributes["urn"]
					return nil
				},
			),
		},
		{
			Config: providerBlock,
			Check: func(*terraform.State) error {
				reqURL := os.Getenv("DATAHUB_GMS_URL") + "/openapi/v3/entity/monitor/" + url.PathEscape(mockMonitorURN(assertionURN))
				resp, err := http.Get(reqURL) //nolint:noctx
				if err != nil {
					return fmt.Errorf("GET monitor for %s: %w", assertionURN, err)
				}
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return fmt.Errorf("monitor for %s: HTTP %d, want 200: the destroy was expected to leave "+
						"the unidentifiable monitor in place (and warn about it)", assertionURN, resp.StatusCode)
				}
				return nil
			},
		},
	}
}

// AssertionMonitorLinkLagImportSteps covers importing an assertion whose
// monitor link is briefly invisible, which is what importing one created
// seconds earlier meets on DataHub Cloud.
//
//  1. Create normally, recording monitor_urn.
//  2. Hide the link for the next three lookups and import. Import starts with
//     no state, so a single empty lookup would leave monitor_urn null; the
//     import must wait the lookups out and record the monitor, which
//     ImportStateVerify then compares against the created state.
//
// Three resolutions is enough to need more than one retry and few enough to
// clear well inside the shortened wait.
func AssertionMonitorLinkLagImportSteps(c MonitorBackedAssertionCase) []resource.TestStep {
	return []resource.TestStep{
		{
			Config:            c.Config,
			ConfigStateChecks: []statecheck.StateCheck{monitorURNCheck(c.Addr)},
		},
		{
			PreConfig:         func() { mockControl(http.MethodPost, "/test-control/hide-monitor-links?resolutions=3") },
			ResourceName:      c.Addr,
			ImportState:       true,
			ImportStateVerify: true,
		},
	}
}

// AssertionMonitorLinkLagImportGivesUpSteps covers an import whose monitor link
// stays invisible past the import wait. The caller must shorten the wait with
// datahub.SetMonitorLinkBackoffForTesting.
//
//  1. Create normally, and note the assertion URN.
//  2. Drop the resource from state without destroying it (a removed block), so
//     the assertion and its monitor exist with nothing tracking them, as they
//     would for a practitioner about to import.
//  3. With every link hidden, import into the real state. The import must
//     succeed (with a warning) and record monitor_urn null rather than fail.
//  4. Once the link is visible, a refresh fills monitor_urn.
//  5. A plan against the same configuration is empty.
//
// The final destroy then runs with the refreshed monitor_urn, and the caller's
// CheckDestroy proves the monitor went with the assertion.
func AssertionMonitorLinkLagImportGivesUpSteps(c MonitorBackedAssertionCase) []resource.TestStep {
	var assertionURN string
	return []resource.TestStep{
		{
			Config: c.Config,
			Check: func(s *terraform.State) error {
				assertionURN = s.RootModule().Resources[c.Addr].Primary.Attributes["urn"]
				return nil
			},
		},
		{
			Config: providerBlock + fmt.Sprintf("removed {\n  from = %s\n  lifecycle {\n    destroy = false\n  }\n}\n", c.Addr),
		},
		{
			PreConfig:          func() { mockControl(http.MethodPost, "/test-control/hide-monitor-links") },
			Config:             c.Config,
			ResourceName:       c.Addr,
			ImportState:        true,
			ImportStateIdFunc:  func(*terraform.State) (string, error) { return assertionURN, nil },
			ImportStatePersist: true,
			ImportStateCheck: func(states []*terraform.InstanceState) error {
				if len(states) != 1 {
					return fmt.Errorf("expected 1 imported instance, got %d", len(states))
				}
				attrs := states[0].Attributes
				if attrs["urn"] != assertionURN {
					return fmt.Errorf("imported urn = %q, want %q", attrs["urn"], assertionURN)
				}
				if got := attrs["monitor_urn"]; got != "" {
					return fmt.Errorf("imported monitor_urn = %q, want empty: every lookup was hidden", got)
				}
				return nil
			},
		},
		{
			PreConfig:    func() { mockControl(http.MethodDelete, "/test-control/hide-monitor-links") },
			RefreshState: true,
			Check:        resource.TestMatchResourceAttr(c.Addr, "monitor_urn", regexp.MustCompile(`^urn:li:monitor:`)),
		},
		{
			Config: c.Config,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{monitorURNCheck(c.Addr)},
		},
	}
}

// AssertionMonitorLinkLagRefreshSteps pins that a normal refresh does not wait
// for the monitor link, even with monitor_urn null: only import waits, so that
// terraform plan stays fast. The caller must shorten the wait with a backoff
// dense enough that waiting would show as many lookups (see
// TestAssertionMonitorLinkLag_RefreshDoesNotWait_mock).
//
//  1. With every link hidden, create; the create's own wait runs out and
//     monitor_urn is recorded null.
//  2. Refresh with the link still hidden, counting lookups. A Read that does
//     not wait makes exactly one, and the step's check runs straight after the
//     single refresh, so one is what it sees today. It allows two so that a
//     framework change adding a refresh does not fail it, while a Read that
//     waited makes well over a hundred against the caller's backoff.
//  3. Reveal the link and refresh, so the final destroy has the monitor URN
//     and the caller's CheckDestroy can prove the monitor went too.
func AssertionMonitorLinkLagRefreshSteps(c MonitorBackedAssertionCase) []resource.TestStep {
	var before int
	return []resource.TestStep{
		{
			PreConfig:         func() { mockControl(http.MethodPost, "/test-control/hide-monitor-links") },
			Config:            c.Config,
			ConfigStateChecks: []statecheck.StateCheck{statecheck.ExpectKnownValue(c.Addr, tfjsonpath.New("monitor_urn"), knownvalue.Null())},
		},
		{
			PreConfig: func() {
				n, err := MonitorLookups()
				if err != nil {
					panic(err)
				}
				before = n
			},
			RefreshState: true,
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckNoResourceAttr(c.Addr, "monitor_urn"),
				func(*terraform.State) error {
					after, err := MonitorLookups()
					if err != nil {
						return err
					}
					if delta := after - before; delta > 2 {
						return fmt.Errorf("refresh made %d monitor lookups, want 1 (at most 2): "+
							"a refresh must not wait for the monitor link", delta)
					}
					return nil
				},
			),
		},
		{
			PreConfig:    func() { mockControl(http.MethodDelete, "/test-control/hide-monitor-links") },
			RefreshState: true,
			Check:        resource.TestMatchResourceAttr(c.Addr, "monitor_urn", regexp.MustCompile(`^urn:li:monitor:`)),
		},
	}
}

// AssertionCreateTagFailureSteps covers a create that fails after the
// assertion was written, at the step that applies the provider's default tags.
// The assertion must still be recorded -- Terraform then holds it as tainted --
// so the next apply replaces it, deleting it and its monitor, rather than
// creating a second assertion beside an untracked one.
//
//  1. Create the marker tag on its own (see tagBootstrapStep).
//  2. Fail the tags write: the apply errors.
//  3. Re-apply: the plan replaces the tainted assertion instead of creating one.
//  4. Turn the tag default off before the framework's destroy, as the other
//     typed assertion tag scenarios do.
func AssertionCreateTagFailureSteps(c MonitorBackedAssertionCase, tagID string) []resource.TestStep {
	tagURN := "urn:li:tag:" + tagID
	with := tagProviderBlock(tagURN) + tagResourceConfig(tagID) + c.Resource
	without := tagProviderBlock("") + tagResourceConfig(tagID) + c.Resource
	return []resource.TestStep{
		tagBootstrapStep(tagID),
		{
			PreConfig:   func() { mockControl(http.MethodPost, "/test-control/fail-next-assertion-tags-write") },
			Config:      with,
			ExpectError: regexp.MustCompile(`simulated assertion\s+aspect write failure`),
		},
		{
			Config: with,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(c.Addr, plancheck.ResourceActionDestroyBeforeCreate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{monitorURNCheck(c.Addr)},
		},
		{
			Config: without,
		},
	}
}
