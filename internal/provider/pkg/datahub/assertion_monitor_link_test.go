// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMonitorLinkBackoffPoll(t *testing.T) {
	fast := monitorLinkBackoff{initial: time.Millisecond, max: 4 * time.Millisecond, budget: 200 * time.Millisecond}

	t.Run("succeeds_after_retries", func(t *testing.T) {
		calls := 0
		err := fast.poll(t.Context(), func() (bool, error) {
			calls++
			return calls < 4, nil
		})
		if err != nil {
			t.Fatalf("poll() error = %v", err)
		}
		if calls != 4 {
			t.Errorf("attempts = %d, want 4", calls)
		}
	})

	t.Run("permanent_error_stops_immediately", func(t *testing.T) {
		calls := 0
		want := errors.New("boom")
		err := fast.poll(t.Context(), func() (bool, error) {
			calls++
			return false, want
		})
		if !errors.Is(err, want) || calls != 1 {
			t.Errorf("poll() = %v after %d attempts, want %v after 1", err, calls, want)
		}
	})

	t.Run("budget_spent_wraps_last_error", func(t *testing.T) {
		last := errors.New("still lagging")
		start := time.Now()
		err := fast.poll(t.Context(), func() (bool, error) { return true, last })
		if !errors.Is(err, errMonitorLinkBudgetSpent) || !errors.Is(err, last) {
			t.Fatalf("poll() = %v, want both errMonitorLinkBudgetSpent and the last error", err)
		}
		if elapsed := time.Since(start); elapsed < fast.budget || elapsed > 10*fast.budget {
			t.Errorf("gave up after %s, want about the %s budget", elapsed, fast.budget)
		}
	})

	t.Run("intervals_grow_to_the_cap", func(t *testing.T) {
		b := monitorLinkBackoff{initial: 10 * time.Millisecond, max: 40 * time.Millisecond, budget: time.Second}
		var stamps []time.Time
		_ = b.poll(t.Context(), func() (bool, error) {
			stamps = append(stamps, time.Now())
			return len(stamps) < 6, nil
		})
		// Gaps: 10, 20, 40, 40, 40 ms. Timers never fire early, so each gap is
		// at least its interval; the cap shows as the later gaps not doubling.
		want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 40 * time.Millisecond, 40 * time.Millisecond}
		for i, w := range want {
			gap := stamps[i+1].Sub(stamps[i])
			if gap < w {
				t.Errorf("gap %d = %s, want at least %s", i, gap, w)
			}
		}
		if total := stamps[len(stamps)-1].Sub(stamps[0]); total > 600*time.Millisecond {
			t.Errorf("five waits took %s; the interval does not appear to be capped at %s", total, b.max)
		}
	})

	t.Run("context_cancel_stops_wait", func(t *testing.T) {
		slow := monitorLinkBackoff{initial: time.Hour, max: time.Hour, budget: time.Hour}
		ctx, cancel := context.WithCancel(t.Context())
		go func() { time.Sleep(20 * time.Millisecond); cancel() }()
		err := slow.poll(ctx, func() (bool, error) { return true, nil })
		if !errors.Is(err, context.Canceled) {
			t.Errorf("poll() = %v, want context.Canceled", err)
		}
	})
}

func TestIsMonitorNotLinkedError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"Failed to upsert Assertion. Monitor for assertion urn:li:assertion:3f1c-ab does not exist.", true},
		{"Monitor for assertion urn:li:assertion:x does not exist", true},
		{"An unknown error occurred. (Error ID: 1a2b3c4d)", false},
		{"Failed to upsert Assertion. Assertion urn:li:assertion:x does not exist.", false},
		{"Entity urn:li:dataset:(urn:li:dataPlatform:hive,t,PROD) does not exist", false},
		{"Monitor for assertion does not exist", false},
		{"Unauthorized to perform this action.", false},
	}
	for _, c := range cases {
		if got := isMonitorNotLinkedError(c.msg); got != c.want {
			t.Errorf("isMonitorNotLinkedError(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
}

// monitorLinkServer answers the volume upsert mutation and the
// assertion.monitor lookup. hiddenLookups lookups see no monitor before the
// link appears (-1: never); refusals updates are refused with the server's
// "monitor does not exist" error, after which updateErr (if set) is returned
// instead of success.
type monitorLinkServer struct {
	mu            sync.Mutex
	hiddenLookups int
	refusals      int
	updateErr     string
	lookups       int
	sends         int
}

const (
	linkTestAssertion = "urn:li:assertion:link-test"
	linkTestMonitor   = "urn:li:monitor:(urn:li:dataset:(urn:li:dataPlatform:hive,t,PROD),m1)"
)

func (s *monitorLinkServer) start(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		s.mu.Lock()
		defer s.mu.Unlock()
		switch {
		case strings.Contains(req.Query, "getAssertionMonitor"):
			s.lookups++
			monitor := `{"urn":"` + linkTestMonitor + `"}`
			if s.hiddenLookups < 0 || s.lookups <= s.hiddenLookups {
				monitor = "null"
			}
			_, _ = w.Write([]byte(`{"data":{"assertion":{"monitor":` + monitor + `}}}`))
		case strings.Contains(req.Query, "upsertDatasetVolumeAssertionMonitor"):
			s.sends++
			if urn, _ := req.Variables["assertionUrn"].(string); urn != "" {
				if s.refusals > 0 {
					s.refusals--
					_, _ = w.Write([]byte(`{"errors":[{"message":"Failed to upsert Assertion. Monitor for assertion ` + urn + ` does not exist."}]}`))
					return
				}
				if s.updateErr != "" {
					_, _ = w.Write([]byte(`{"errors":[{"message":"` + s.updateErr + `"}]}`))
					return
				}
			}
			_, _ = w.Write([]byte(`{"data":{"upsertDatasetVolumeAssertionMonitor":{"urn":"` + linkTestAssertion + `"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return newTestClient(t, srv)
}

func fastMonitorLinkWait(t *testing.T) {
	t.Helper()
	t.Cleanup(SetMonitorLinkBackoffForTesting(time.Millisecond, 4*time.Millisecond, 150*time.Millisecond))
}

var linkTestVolumeInput = VolumeAssertionInput{
	EntityURN: "urn:li:dataset:(urn:li:dataPlatform:hive,t,PROD)", VolumeType: "ROW_COUNT_TOTAL",
	Operator: "GREATER_THAN", SingleValue: "1", EvaluationCron: "0 * * * *", EvaluationTimezone: "UTC",
	SourceType: "INFORMATION_SCHEMA", Mode: "ACTIVE",
}

func TestUpsertAssertionMonitor_CreateWaitsForLink(t *testing.T) {
	fastMonitorLinkWait(t)
	s := &monitorLinkServer{hiddenLookups: 3}
	c := s.start(t)

	urn, monitor, err := c.UpsertVolumeAssertion(t.Context(), linkTestVolumeInput)
	if err != nil {
		t.Fatalf("UpsertVolumeAssertion() error = %v", err)
	}
	if urn != linkTestAssertion || monitor != linkTestMonitor {
		t.Errorf("got (%q, %q), want (%q, %q)", urn, monitor, linkTestAssertion, linkTestMonitor)
	}
	if s.lookups != 4 {
		t.Errorf("lookups = %d, want 4 (three hidden, then found)", s.lookups)
	}
}

// TestUpsertAssertionMonitor_CreateLinkNeverAppears pins the leak fix at the
// client: when the wait runs out, the assertion URN still comes back, with a
// *MonitorPendingError the resource turns into a warning.
func TestUpsertAssertionMonitor_CreateLinkNeverAppears(t *testing.T) {
	fastMonitorLinkWait(t)
	s := &monitorLinkServer{hiddenLookups: -1}
	c := s.start(t)

	urn, monitor, err := c.UpsertVolumeAssertion(t.Context(), linkTestVolumeInput)
	var pending *MonitorPendingError
	if !errors.As(err, &pending) {
		t.Fatalf("UpsertVolumeAssertion() error = %v, want *MonitorPendingError", err)
	}
	if urn != linkTestAssertion || pending.AssertionURN != linkTestAssertion {
		t.Errorf("assertion URN = %q (error says %q), want %q returned despite the error", urn, pending.AssertionURN, linkTestAssertion)
	}
	if monitor != "" {
		t.Errorf("monitor URN = %q, want empty", monitor)
	}
	if !errors.Is(err, errMonitorLinkBudgetSpent) {
		t.Errorf("error %v does not record that the wait ran out", err)
	}
	if s.sends != 1 {
		t.Errorf("mutation sent %d times, want once: a create must never be resent", s.sends)
	}
}

func TestUpsertAssertionMonitor_UpdateRetriesRefusal(t *testing.T) {
	fastMonitorLinkWait(t)
	s := &monitorLinkServer{refusals: 3}
	c := s.start(t)

	in := linkTestVolumeInput
	in.AssertionURN = linkTestAssertion
	urn, _, err := c.UpsertVolumeAssertion(t.Context(), in)
	if err != nil {
		t.Fatalf("UpsertVolumeAssertion() error = %v", err)
	}
	if urn != linkTestAssertion {
		t.Errorf("urn = %q, want %q", urn, linkTestAssertion)
	}
	if s.sends != 4 {
		t.Errorf("mutation sent %d times, want 4 (three refusals, then success)", s.sends)
	}
}

// TestUpsertAssertionMonitor_UpdateWaitsForLinkFirst covers the update path's
// wait for the link before sending, which avoids the refusal even on servers
// that hide its text behind a generic error message.
func TestUpsertAssertionMonitor_UpdateWaitsForLinkFirst(t *testing.T) {
	fastMonitorLinkWait(t)
	s := &monitorLinkServer{hiddenLookups: 2}
	c := s.start(t)

	in := linkTestVolumeInput
	in.AssertionURN = linkTestAssertion
	if _, _, err := c.UpsertVolumeAssertion(t.Context(), in); err != nil {
		t.Fatalf("UpsertVolumeAssertion() error = %v", err)
	}
	if s.lookups != 3 || s.sends != 1 {
		t.Errorf("lookups = %d, sends = %d; want 3 lookups (two hidden) then a single send", s.lookups, s.sends)
	}
}

func TestUpsertAssertionMonitor_UpdateGivesUp(t *testing.T) {
	fastMonitorLinkWait(t)
	s := &monitorLinkServer{refusals: 1 << 20}
	c := s.start(t)

	in := linkTestVolumeInput
	in.AssertionURN = linkTestAssertion
	_, _, err := c.UpsertVolumeAssertion(t.Context(), in)
	if err == nil {
		t.Fatal("UpsertVolumeAssertion() = nil error, want failure once the budget is spent")
	}
	for _, want := range []string{"does not exist", "has not yet indexed the link", "retry the apply"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	var pending *MonitorPendingError
	if errors.As(err, &pending) {
		t.Error("an update failure must not be a *MonitorPendingError: nothing was created")
	}
}

func TestUpsertAssertionMonitor_UpdateDoesNotRetryOtherErrors(t *testing.T) {
	fastMonitorLinkWait(t)
	s := &monitorLinkServer{updateErr: "An unknown error occurred. (Error ID: 1a2b3c4d)"}
	c := s.start(t)

	in := linkTestVolumeInput
	in.AssertionURN = linkTestAssertion
	_, _, err := c.UpsertVolumeAssertion(t.Context(), in)
	if err == nil || !strings.Contains(err.Error(), "An unknown error occurred") {
		t.Fatalf("UpsertVolumeAssertion() error = %v, want the server's error", err)
	}
	if s.sends != 1 {
		t.Errorf("mutation sent %d times, want once: only the monitor refusal is retried", s.sends)
	}
}
