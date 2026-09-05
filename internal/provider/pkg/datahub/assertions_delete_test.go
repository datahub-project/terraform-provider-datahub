// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// deleteMonitorTestServer is a minimal DataHub stand-in for exercising
// DeleteCloudAssertionWithMonitor. It records the order of the calls it
// receives so tests can assert not just the outcome but what was (and was not)
// deleted -- the defect being guarded against is precisely a call that never
// happens or happens after the point of no return.
type deleteMonitorTestServer struct {
	mu     sync.Mutex
	events []string

	// lookupMonitorURN is returned by the getAssertionMonitor query; empty
	// means "assertion has no monitor" (data.assertion.monitor = null).
	lookupMonitorURN string
	// lookupFails makes the getAssertionMonitor query return a GraphQL error.
	lookupFails bool
	// monitorDeleteStatus is the HTTP status for DELETE on the monitor entity.
	monitorDeleteStatus int
}

func (s *deleteMonitorTestServer) record(event string) {
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()
}

func (s *deleteMonitorTestServer) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...)
}

func (s *deleteMonitorTestServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/graphql"):
			body, _ := io.ReadAll(r.Body)
			q := string(body)
			switch {
			case strings.Contains(q, "getAssertionMonitor"):
				s.record("lookupMonitor")
				if s.lookupFails {
					_, _ = w.Write([]byte(`{"errors":[{"message":"search backend unavailable"}]}`))
					return
				}
				if s.lookupMonitorURN == "" {
					_, _ = w.Write([]byte(`{"data":{"assertion":{"monitor":null}}}`))
					return
				}
				_, _ = w.Write([]byte(`{"data":{"assertion":{"monitor":{"urn":"` + s.lookupMonitorURN + `"}}}}`))
			case strings.Contains(q, "deleteAssertion"):
				s.record("deleteAssertion")
				_, _ = w.Write([]byte(`{"data":{"deleteAssertion":true}}`))
			default:
				t.Errorf("unexpected GraphQL query: %s", q)
				http.Error(w, "unexpected query", http.StatusBadRequest)
			}
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/monitor/"):
			s.record("deleteMonitor")
			status := s.monitorDeleteStatus
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func assertEvents(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("calls = %v, want %v", got, want)
		}
	}
}

// TestDeleteCloudAssertionWithMonitor_KnownMonitorURN verifies that when the
// caller supplies the monitor URN (persisted in Terraform state at create
// time), no lookup is performed and the monitor is deleted BEFORE the
// assertion -- the ordering that keeps a partial failure retryable.
func TestDeleteCloudAssertionWithMonitor_KnownMonitorURN(t *testing.T) {
	srv := &deleteMonitorTestServer{}
	c := newTestClient(t, srv.start(t))

	err := c.DeleteCloudAssertionWithMonitor(t.Context(), "urn:li:assertion:a1", "urn:li:monitor:m1")
	if err != nil {
		t.Fatalf("DeleteCloudAssertionWithMonitor() error = %v", err)
	}
	assertEvents(t, srv.recorded(), []string{"deleteMonitor", "deleteAssertion"})
}

// TestDeleteCloudAssertionWithMonitor_FallbackLookup verifies the legacy-state
// path: with no monitor URN supplied, the monitor is resolved via the lookup
// and then both entities are deleted, monitor first.
func TestDeleteCloudAssertionWithMonitor_FallbackLookup(t *testing.T) {
	srv := &deleteMonitorTestServer{lookupMonitorURN: "urn:li:monitor:m1"}
	c := newTestClient(t, srv.start(t))

	err := c.DeleteCloudAssertionWithMonitor(t.Context(), "urn:li:assertion:a1", "")
	if err != nil {
		t.Fatalf("DeleteCloudAssertionWithMonitor() error = %v", err)
	}
	assertEvents(t, srv.recorded(), []string{"lookupMonitor", "deleteMonitor", "deleteAssertion"})
}

// TestDeleteCloudAssertionWithMonitor_FallbackLookupError verifies the
// hardened error path: when the fallback lookup fails, the delete aborts
// before removing ANYTHING -- proceeding would delete the assertion and leak
// the monitor as an orphan (OBS-2077) -- and the error tells the user why and
// that a retry is safe.
func TestDeleteCloudAssertionWithMonitor_FallbackLookupError(t *testing.T) {
	srv := &deleteMonitorTestServer{lookupFails: true}
	c := newTestClient(t, srv.start(t))

	err := c.DeleteCloudAssertionWithMonitor(t.Context(), "urn:li:assertion:a1", "")
	if err == nil {
		t.Fatal("DeleteCloudAssertionWithMonitor() = nil error, want failure when the monitor lookup errors")
	}
	if !strings.Contains(err.Error(), "orphan") {
		t.Errorf("error %q does not explain the orphaned-monitor consequence", err)
	}
	if !strings.Contains(err.Error(), "Retry") && !strings.Contains(err.Error(), "retry") {
		t.Errorf("error %q does not tell the user a retry is safe", err)
	}
	assertEvents(t, srv.recorded(), []string{"lookupMonitor"})
}

// TestDeleteCloudAssertionWithMonitor_NoMonitor verifies that an empty
// fallback lookup result (no error) means the assertion genuinely has no
// monitor: the assertion alone is deleted and no monitor delete is attempted.
func TestDeleteCloudAssertionWithMonitor_NoMonitor(t *testing.T) {
	srv := &deleteMonitorTestServer{lookupMonitorURN: ""}
	c := newTestClient(t, srv.start(t))

	err := c.DeleteCloudAssertionWithMonitor(t.Context(), "urn:li:assertion:a1", "")
	if err != nil {
		t.Fatalf("DeleteCloudAssertionWithMonitor() error = %v", err)
	}
	assertEvents(t, srv.recorded(), []string{"lookupMonitor", "deleteAssertion"})
}

// TestDeleteCloudAssertionWithMonitor_MonitorAlreadyDeleted verifies that an
// absent monitor (HTTP 404) is success, not an error: DataHub Cloud's
// server-side MonitorDeletionHook may delete the monitor first, and a
// previous partially-failed destroy may already have removed it. The
// assertion delete must still proceed.
func TestDeleteCloudAssertionWithMonitor_MonitorAlreadyDeleted(t *testing.T) {
	srv := &deleteMonitorTestServer{monitorDeleteStatus: http.StatusNotFound}
	c := newTestClient(t, srv.start(t))

	err := c.DeleteCloudAssertionWithMonitor(t.Context(), "urn:li:assertion:a1", "urn:li:monitor:m1")
	if err != nil {
		t.Fatalf("DeleteCloudAssertionWithMonitor() error = %v, want already-deleted monitor treated as success", err)
	}
	assertEvents(t, srv.recorded(), []string{"deleteMonitor", "deleteAssertion"})
}

// TestDeleteCloudAssertionWithMonitor_MonitorDeleteError verifies that a
// non-404 monitor delete failure is surfaced (it used to be discarded, which
// is how orphans accumulated) and that the assertion is NOT deleted, so the
// resource stays in state and a retry can converge.
func TestDeleteCloudAssertionWithMonitor_MonitorDeleteError(t *testing.T) {
	srv := &deleteMonitorTestServer{monitorDeleteStatus: http.StatusInternalServerError}
	c := newTestClient(t, srv.start(t))

	err := c.DeleteCloudAssertionWithMonitor(t.Context(), "urn:li:assertion:a1", "urn:li:monitor:m1")
	if err == nil {
		t.Fatal("DeleteCloudAssertionWithMonitor() = nil error, want failure when the monitor delete errors")
	}
	assertEvents(t, srv.recorded(), []string{"deleteMonitor"})
}
