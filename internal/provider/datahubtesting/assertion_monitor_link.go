// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// Monitor link lag.
//
// On DataHub Cloud an upsertDataset*AssertionMonitor mutation commits the
// assertion and its monitor together, but the link from the assertion to the
// monitor (the monitor's Evaluates relationship) is resolved through the graph
// index, which for API callers is updated asynchronously. Until it is, two
// things the provider relies on see no monitor:
//
//   - the assertion.monitor GraphQL field returns null, without an error;
//   - an update through the same mutation fails before writing anything, with
//     "Failed to upsert Assertion. Monitor for assertion <urn> does not exist."
//
// The monitor entity itself is there all along: reading or deleting it by URN
// through the OpenAPI v3 entity endpoint works, because that reads the primary
// store rather than the index.
//
// The mock reproduces both effects, and only those, under two test controls:
//
//	POST   /test-control/hide-monitor-links               hide every link until revealed
//	POST   /test-control/hide-monitor-links?resolutions=N hide for the next N resolutions
//	DELETE /test-control/hide-monitor-links               reveal
//	POST   /test-control/fail-monitor-updates?count=N     refuse the next N updates
//
// A resolution is either an assertion.monitor lookup or an update mutation,
// the two operations that walk the link. fail-monitor-updates refuses updates
// while leaving lookups untouched, which is the window in which the provider's
// lookup and the server's own read disagree: the provider has to recognise the
// refusal itself rather than rely on having seen the link first.

// monitorNotLinkedMessage is the error DataHub Cloud returns when an assertion
// update cannot resolve the assertion's monitor.
func monitorNotLinkedMessage(assertionURN string) string {
	return fmt.Sprintf("Failed to upsert Assertion. Monitor for assertion %s does not exist.", assertionURN)
}

// monitorLinkHiddenLocked reports whether the current resolution of an
// assertion-to-monitor link misses it, consuming one counted resolution if a
// count is armed. Callers hold s.mu.
func (s *mockServer) monitorLinkHiddenLocked() bool {
	if s.hideMonitorLinks {
		return true
	}
	if s.hideMonitorLinkResolutions > 0 {
		s.hideMonitorLinkResolutions--
		return true
	}
	return false
}

// refuseUnlinkedMonitorUpdateLocked writes DataHub Cloud's refusal and returns
// true when an update of assertionURN (non-empty on update, empty on create)
// cannot see the assertion's monitor. Like the server, it refuses before
// changing anything. Callers hold s.mu.
func (s *mockServer) refuseUnlinkedMonitorUpdateLocked(w http.ResponseWriter, assertionURN string) bool {
	if assertionURN == "" {
		return false
	}
	refuse := s.monitorLinkHiddenLocked()
	if !refuse && s.failMonitorUpdates > 0 {
		s.failMonitorUpdates--
		refuse = true
	}
	if !refuse {
		return false
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]any{{"message": monitorNotLinkedMessage(assertionURN)}},
	})
	return true
}

// handleHideMonitorLinks arms and disarms link hiding. See the comment above.
func (s *mockServer) handleHideMonitorLinks(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case http.MethodPost:
		if raw := r.URL.Query().Get("resolutions"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 0 {
				http.Error(w, "resolutions must be a non-negative integer", http.StatusBadRequest)
				return
			}
			s.hideMonitorLinkResolutions = n
		} else {
			s.hideMonitorLinks = true
		}
	case http.MethodDelete:
		s.hideMonitorLinks = false
		s.hideMonitorLinkResolutions = 0
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleFailMonitorUpdates arms a count of update refusals. See the comment
// above.
func (s *mockServer) handleFailMonitorUpdates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	n, err := strconv.Atoi(r.URL.Query().Get("count"))
	if err != nil || n < 0 {
		http.Error(w, "count must be a non-negative integer", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.failMonitorUpdates = n
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
