// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// mockControl sends a method-only request to a mock /test-control endpoint.
func mockControl(t *testing.T, method, url string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, url, nil)
	if err != nil {
		t.Fatalf("mockControl: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("mockControl %s %s: %v", method, url, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("mockControl %s %s: unexpected status %d", method, url, resp.StatusCode)
	}
}

// monitorExistsOnMock reports whether the mock still holds the monitor entity.
func monitorExistsOnMock(t *testing.T, baseURL, monitorURN string) bool {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+"/openapi/v3/entity/monitor/"+monitorURN, nil)
	if err != nil {
		t.Fatalf("monitorExistsOnMock: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("monitorExistsOnMock: %v", err)
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func createMockFreshnessAssertion(t *testing.T, c *datahub.Client) (string, string) {
	t.Helper()
	urn, monitorURN, err := c.UpsertFreshnessAssertion(t.Context(), datahub.FreshnessAssertionInput{
		EntityURN:          "urn:li:dataset:(urn:li:dataPlatform:hive,delete.mock.table,PROD)",
		ScheduleType:       "SINCE_THE_LAST_CHECK",
		EvaluationCron:     "0 */8 * * *",
		EvaluationTimezone: "UTC",
		SourceType:         "DATAHUB_OPERATION",
		Mode:               "ACTIVE",
	})
	if err != nil {
		t.Fatalf("UpsertFreshnessAssertion: %v", err)
	}
	if monitorURN == "" {
		t.Fatal("UpsertFreshnessAssertion returned an empty monitor URN on create")
	}
	return urn, monitorURN
}

// TestDeleteCloudAssertionWithMonitor_FallbackFailureAgainstMock exercises the
// legacy-state delete path end-to-end against the shared mock server rather
// than a hand-rolled stub: with the monitor lookup failing (the mock's
// fail-monitor-lookup control models the live query's transient graph/search
// failures), the delete must abort with the orphan explanation and leave BOTH
// entities in place; once the lookup recovers, a retry of the same call must
// converge and remove both. This is the exact sequence a user with pre-upgrade
// state hits during an index-lag window.
func TestDeleteCloudAssertionWithMonitor_FallbackFailureAgainstMock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	c, err := datahub.NewClient(server.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := t.Context()
	urn, monitorURN := createMockFreshnessAssertion(t, c)

	// Arm the lookup failure and attempt a delete with no persisted monitor URN.
	mockControl(t, http.MethodPost, server.URL+"/test-control/fail-monitor-lookup")
	delErr := c.DeleteCloudAssertionWithMonitor(ctx, urn, "")
	if delErr == nil {
		t.Fatal("DeleteCloudAssertionWithMonitor() = nil error while the monitor lookup fails, want an aborted delete")
	}
	if !strings.Contains(delErr.Error(), "orphan") {
		t.Errorf("error %q does not explain the orphaned-monitor consequence", delErr)
	}
	mockControl(t, http.MethodDelete, server.URL+"/test-control/fail-monitor-lookup")

	// The aborted delete must have removed nothing.
	if a, getErr := c.GetAssertionByURN(ctx, urn); getErr != nil || a == nil {
		t.Fatalf("assertion %q missing after aborted delete (a=%v, err=%v); the abort must precede any deletion", urn, a, getErr)
	}
	if !monitorExistsOnMock(t, server.URL, monitorURN) {
		t.Fatalf("monitor %q missing after aborted delete; the abort must precede any deletion", monitorURN)
	}

	// With the lookup healthy again the same call converges.
	if retryErr := c.DeleteCloudAssertionWithMonitor(ctx, urn, ""); retryErr != nil {
		t.Fatalf("retry after lookup recovery: %v", retryErr)
	}
	if a, getErr := c.GetAssertionByURN(ctx, urn); getErr != nil {
		t.Fatalf("GetAssertionByURN after delete: %v", getErr)
	} else if a != nil {
		t.Errorf("assertion %q still exists after delete", urn)
	}
	if monitorExistsOnMock(t, server.URL, monitorURN) {
		t.Errorf("monitor %q still exists after delete -- orphaned", monitorURN)
	}
}

// TestDeleteCloudAssertionWithMonitor_HookWonRaceAgainstMock verifies against
// the shared mock that a monitor already deleted out-of-band (as DataHub
// Cloud's MonitorDeletionHook does) is treated as success: the mock's monitor
// DELETE returns 404 for an absent entity, and the assertion delete must still
// proceed.
func TestDeleteCloudAssertionWithMonitor_HookWonRaceAgainstMock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	c, err := datahub.NewClient(server.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := t.Context()
	urn, monitorURN := createMockFreshnessAssertion(t, c)

	// Simulate the server-side hook (or an earlier partial destroy) having
	// already removed the monitor.
	if err := c.DeleteMonitor(ctx, monitorURN); err != nil {
		t.Fatalf("out-of-band DeleteMonitor: %v", err)
	}

	if err := c.DeleteCloudAssertionWithMonitor(ctx, urn, monitorURN); err != nil {
		t.Fatalf("DeleteCloudAssertionWithMonitor() error = %v, want absent monitor treated as success", err)
	}
	if a, getErr := c.GetAssertionByURN(ctx, urn); getErr != nil {
		t.Fatalf("GetAssertionByURN after delete: %v", getErr)
	} else if a != nil {
		t.Errorf("assertion %q still exists after delete", urn)
	}
}
