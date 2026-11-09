// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// shortMonitorLinkWait shrinks the provider's wait for an assertion's monitor
// link from minutes to a fraction of a second, so a scenario can run the wait
// out without sleeping through the real budget.
func shortMonitorLinkWait(t *testing.T) {
	t.Helper()
	t.Cleanup(datahub.SetMonitorLinkBackoffForTesting(5*time.Millisecond, 20*time.Millisecond, 250*time.Millisecond))
}

// TestAssertionMonitorLinkLag_Create_mock verifies that a create whose monitor
// link DataHub has not indexed by the end of the wait records the assertion
// (with a warning and monitor_urn null) instead of failing and leaving the
// assertion and its monitor behind untracked, and that a later refresh fills
// monitor_urn without a diff. Runs against every monitor-backed resource,
// since each has its own Create.
func TestAssertionMonitorLinkLag_Create_mock(t *testing.T) {
	for _, c := range datahubtesting.MonitorBackedAssertionCases() {
		t.Run(c.Name, func(t *testing.T) {
			shortMonitorLinkWait(t)
			server := datahubtesting.NewServer(t)
			t.Setenv("DATAHUB_GMS_URL", server.URL)
			t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				CheckDestroy:             c.CheckDestroy,
				Steps:                    datahubtesting.AssertionMonitorLinkLagCreateSteps(c),
			})
		})
	}
}

// TestAssertionMonitorLinkLag_Update_mock verifies that an update refused
// because the server cannot yet resolve the assertion's monitor is resent
// until it succeeds, and gives up with an explanatory error at the end of the
// budget. Runs against every monitor-backed resource, since each sends its own
// update.
func TestAssertionMonitorLinkLag_Update_mock(t *testing.T) {
	for _, c := range datahubtesting.MonitorBackedAssertionCases() {
		t.Run(c.Name, func(t *testing.T) {
			shortMonitorLinkWait(t)
			server := datahubtesting.NewServer(t)
			t.Setenv("DATAHUB_GMS_URL", server.URL)
			t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				CheckDestroy:             c.CheckDestroy,
				Steps:                    datahubtesting.AssertionMonitorLinkLagUpdateSteps(c),
			})
		})
	}
}

// TestAssertionMonitorLinkLag_Destroy_mock verifies that destroying an
// assertion whose monitor could not be identified goes ahead rather than
// failing. Delete is shared by the five resources, so one covers it.
func TestAssertionMonitorLinkLag_Destroy_mock(t *testing.T) {
	shortMonitorLinkWait(t)
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	c := datahubtesting.MonitorBackedAssertionCases()[0]
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             c.CheckDestroy,
		Steps:                    datahubtesting.AssertionMonitorLinkLagDestroySteps(c),
	})
}

// TestAssertionMonitorLinkLag_Import_mock verifies that importing an
// assertion whose monitor link is briefly invisible waits for it and records
// monitor_urn, so ImportStateVerify matches the created state. Runs against
// every monitor-backed resource, since each has its own ImportState.
func TestAssertionMonitorLinkLag_Import_mock(t *testing.T) {
	for _, c := range datahubtesting.MonitorBackedAssertionCases() {
		t.Run(c.Name, func(t *testing.T) {
			shortMonitorLinkWait(t)
			server := datahubtesting.NewServer(t)
			t.Setenv("DATAHUB_GMS_URL", server.URL)
			t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				CheckDestroy:             c.CheckDestroy,
				Steps:                    datahubtesting.AssertionMonitorLinkLagImportSteps(c),
			})
		})
	}
}

// TestAssertionMonitorLinkLag_ImportGivesUp_mock verifies that an import whose
// monitor link outlasts the wait succeeds with monitor_urn null, and that a
// later refresh fills it without a diff. Runs against every monitor-backed
// resource, since each has its own ImportState.
func TestAssertionMonitorLinkLag_ImportGivesUp_mock(t *testing.T) {
	for _, c := range datahubtesting.MonitorBackedAssertionCases() {
		t.Run(c.Name, func(t *testing.T) {
			shortMonitorLinkWait(t)
			server := datahubtesting.NewServer(t)
			t.Setenv("DATAHUB_GMS_URL", server.URL)
			t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				CheckDestroy:             c.CheckDestroy,
				Steps:                    datahubtesting.AssertionMonitorLinkLagImportGivesUpSteps(c),
			})
		})
	}
}

// TestAssertionMonitorLinkLag_RefreshDoesNotWait_mock verifies that a normal
// refresh with monitor_urn null makes one lookup per Read rather than waiting
// for the link. The backoff here retries every millisecond, so a Read that
// waited would make hundreds of lookups within its budget. Runs against every
// monitor-backed resource, since each has its own Read.
func TestAssertionMonitorLinkLag_RefreshDoesNotWait_mock(t *testing.T) {
	for _, c := range datahubtesting.MonitorBackedAssertionCases() {
		t.Run(c.Name, func(t *testing.T) {
			t.Cleanup(datahub.SetMonitorLinkBackoffForTesting(time.Millisecond, time.Millisecond, 250*time.Millisecond))
			server := datahubtesting.NewServer(t)
			t.Setenv("DATAHUB_GMS_URL", server.URL)
			t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				CheckDestroy:             c.CheckDestroy,
				Steps:                    datahubtesting.AssertionMonitorLinkLagRefreshSteps(c),
			})
		})
	}
}

// TestAssertionCreateTagFailure_mock verifies that a create failing after the
// assertion was written records the assertion (tainted) so the next apply
// replaces it, instead of leaving it and its monitor behind untracked. Runs
// against every monitor-backed resource, since each has its own Create.
func TestAssertionCreateTagFailure_mock(t *testing.T) {
	for _, c := range datahubtesting.MonitorBackedAssertionCases() {
		t.Run(c.Name, func(t *testing.T) {
			server := datahubtesting.NewServer(t)
			t.Setenv("DATAHUB_GMS_URL", server.URL)
			t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				CheckDestroy:             c.CheckDestroy,
				Steps:                    datahubtesting.AssertionCreateTagFailureSteps(c, "tf-test-assertion-tag-failure"),
			})
		})
	}
}
