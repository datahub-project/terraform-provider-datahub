// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetAssertionMonitor verifies that GetAssertionMonitor resolves the
// assertion's monitor URN via the getAssertionMonitor query and then parses the
// evaluation schedule, source type, and mode out of the Monitor entity's
// monitorInfo aspect for the matching assertion. These fields live only in the
// Monitor entity, so this is what makes monitor-assertion ImportState complete.
func TestGetAssertionMonitor(t *testing.T) {
	const assertionURN = "urn:li:assertion:abc"
	const monitorURN = "urn:li:monitor:(urn:li:dataset:(urn:li:dataPlatform:bigquery,db.t,PROD),m1)"

	monitorBody := `{
	  "urn": "` + monitorURN + `",
	  "monitorInfo": { "value": {
	    "status": { "mode": "ACTIVE" },
	    "assertionMonitor": { "assertions": [
	      { "assertion": "urn:li:assertion:other",
	        "schedule": { "cron": "0 0 * * *", "timezone": "America/New_York" } },
	      { "assertion": "` + assertionURN + `",
	        "schedule": { "cron": "0 */8 * * *", "timezone": "UTC" },
	        "parameters": { "datasetVolumeParameters": { "sourceType": "INFORMATION_SCHEMA" } } }
	    ] }
	  } }
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/graphql":
			_, _ = w.Write([]byte(`{"data":{"assertion":{"monitor":{"urn":"` + monitorURN + `"}}}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/monitor/"):
			_, _ = w.Write([]byte(monitorBody))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c := newTestClient(t, server)
	mon, err := c.GetAssertionMonitor(t.Context(), assertionURN)
	if err != nil {
		t.Fatalf("GetAssertionMonitor() error = %v", err)
	}
	if mon == nil {
		t.Fatal("GetAssertionMonitor() = nil, want monitor info")
	}
	if mon.EvaluationCron != "0 */8 * * *" {
		t.Errorf("EvaluationCron = %q, want %q (must pick the matching assertion, not the first)", mon.EvaluationCron, "0 */8 * * *")
	}
	if mon.EvaluationTimezone != "UTC" {
		t.Errorf("EvaluationTimezone = %q, want UTC", mon.EvaluationTimezone)
	}
	if mon.SourceType != "INFORMATION_SCHEMA" {
		t.Errorf("SourceType = %q, want INFORMATION_SCHEMA", mon.SourceType)
	}
	if mon.Mode != "ACTIVE" {
		t.Errorf("Mode = %q, want ACTIVE", mon.Mode)
	}
}

// TestGetAssertionMonitor_NoMonitor verifies that an assertion with no linked
// monitor (e.g. a custom assertion) returns (nil, nil) rather than erroring.
func TestGetAssertionMonitor_NoMonitor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"assertion":{"monitor":null}}}`))
	}))
	defer server.Close()

	c := newTestClient(t, server)
	mon, err := c.GetAssertionMonitor(t.Context(), "urn:li:assertion:custom")
	if err != nil {
		t.Fatalf("GetAssertionMonitor() error = %v", err)
	}
	if mon != nil {
		t.Errorf("GetAssertionMonitor() = %+v, want nil for assertion with no monitor", mon)
	}
}
