// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

func TestAcceptMonitorPending(t *testing.T) {
	t.Run("pending_becomes_warning", func(t *testing.T) {
		var diags diag.Diagnostics
		err := acceptMonitorPending(&datahub.MonitorPendingError{
			AssertionURN: "urn:li:assertion:a1",
			Cause:        errors.New("gave up waiting after 3m0s"),
		}, "datahub_volume_assertion", &diags)
		if err != nil {
			t.Fatalf("acceptMonitorPending() = %v, want nil so the assertion is recorded", err)
		}
		if diags.HasError() || diags.WarningsCount() != 1 {
			t.Fatalf("diags = %v, want exactly one warning and no error", diags)
		}
		detail := diags.Warnings()[0].Detail()
		for _, want := range []string{"urn:li:assertion:a1", "datahub_volume_assertion", "monitor_urn", "refresh", "several minutes"} {
			if !strings.Contains(detail, want) {
				t.Errorf("warning detail does not mention %q:\n%s", want, detail)
			}
		}
	})

	t.Run("other_errors_pass_through", func(t *testing.T) {
		var diags diag.Diagnostics
		want := errors.New("DataHub API error: boom")
		if err := acceptMonitorPending(want, "datahub_volume_assertion", &diags); !errors.Is(err, want) {
			t.Errorf("acceptMonitorPending() = %v, want %v unchanged", err, want)
		}
		if len(diags) != 0 {
			t.Errorf("diags = %v, want none", diags)
		}
	})
}

// TestDeleteAssertionAndMonitor_WarnsWhenMonitorUnknown covers the destroy of
// an assertion whose monitor_urn was never recorded and whose link is still
// not visible: the delete goes ahead, with a warning that a monitor may
// remain. A recorded monitor_urn, or one the lookup finds, gets no warning.
func TestDeleteAssertionAndMonitor_WarnsWhenMonitorUnknown(t *testing.T) {
	cases := []struct {
		name        string
		stored      types.String
		lookup      string
		wantWarning bool
	}{
		{"recorded", types.StringValue("urn:li:monitor:m1"), "", false},
		{"found_by_lookup", types.StringNull(), `{"urn":"urn:li:monitor:m1"}`, false},
		{"unknown", types.StringNull(), "null", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodDelete:
					w.WriteHeader(http.StatusOK)
				case r.URL.Path == "/api/graphql":
					body, _ := io.ReadAll(r.Body)
					if strings.Contains(string(body), "getAssertionMonitor") {
						_, _ = w.Write([]byte(`{"data":{"assertion":{"monitor":` + c.lookup + `}}}`))
						return
					}
					_, _ = w.Write([]byte(`{"data":{"deleteAssertion":true}}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			client, err := datahub.NewClient(srv.URL, "test-token")
			if err != nil {
				t.Fatal(err)
			}

			var diags diag.Diagnostics
			if !deleteAssertionAndMonitor(t.Context(), client, "datahub_volume_assertion", "urn:li:assertion:a1", c.stored, &diags) {
				t.Fatalf("deleteAssertionAndMonitor() = false, diags = %v; the destroy must not be blocked", diags)
			}
			if got := diags.WarningsCount() == 1; got != c.wantWarning {
				t.Errorf("warnings = %v, want a warning: %v", diags.Warnings(), c.wantWarning)
			}
		})
	}
}

// TestImportMonitorURN covers the monitor_urn an import starts with: the
// monitor once the link is visible, null with a warning when the wait runs
// out, an error when the lookup fails, and null at once, without waiting or
// any diagnostic, for an assertion the source guard will refuse.
func TestImportMonitorURN(t *testing.T) {
	t.Cleanup(datahub.SetMonitorLinkBackoffForTesting(time.Millisecond, 4*time.Millisecond, 100*time.Millisecond))
	const assertionURN = "urn:li:assertion:a1"
	const monitorURN = "urn:li:monitor:m1"

	cases := []struct {
		name string
		// source is the assertion's source; hidden is how many lookups see no
		// monitor first (-1: all of them); lookupErr makes every lookup fail.
		source      string
		hidden      int
		lookupErr   bool
		want        types.String
		wantLookups int // -1: more than one, the exact count depending on timing
		wantWarning bool
		wantError   bool
	}{
		{name: "linked_after_lag", source: "NATIVE", hidden: 2, want: types.StringValue(monitorURN), wantLookups: 3},
		{name: "empty_source_waits_too", source: "", hidden: 1, want: types.StringValue(monitorURN), wantLookups: 2},
		{name: "never_linked", source: "NATIVE", hidden: -1, want: types.StringNull(), wantLookups: -1, wantWarning: true},
		{name: "lookup_fails", source: "NATIVE", lookupErr: true, want: types.StringNull(), wantLookups: 1, wantError: true},
		{name: "external_skips_wait", source: "EXTERNAL", hidden: -1, want: types.StringNull(), wantLookups: 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var mu sync.Mutex
			lookups := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				body, _ := io.ReadAll(r.Body)
				if r.URL.Path != "/api/graphql" || !strings.Contains(string(body), "getAssertionMonitor") {
					http.NotFound(w, r)
					return
				}
				mu.Lock()
				defer mu.Unlock()
				lookups++
				switch {
				case c.lookupErr:
					_, _ = w.Write([]byte(`{"errors":[{"message":"simulated lookup failure"}]}`))
				case c.hidden < 0 || lookups <= c.hidden:
					_, _ = w.Write([]byte(`{"data":{"assertion":{"monitor":null}}}`))
				default:
					_, _ = w.Write([]byte(`{"data":{"assertion":{"monitor":{"urn":"` + monitorURN + `"}}}}`))
				}
			}))
			defer srv.Close()
			client, err := datahub.NewClient(srv.URL, "test-token")
			if err != nil {
				t.Fatal(err)
			}

			var diags diag.Diagnostics
			got := importMonitorURN(t.Context(), client, "datahub_freshness_assertion",
				&datahub.AssertionInfo{URN: assertionURN, Source: c.source}, &diags)
			if !got.Equal(c.want) {
				t.Errorf("monitor_urn = %v, want %v", got, c.want)
			}
			mu.Lock()
			n := lookups
			mu.Unlock()
			if c.wantLookups >= 0 && n != c.wantLookups {
				t.Errorf("lookups = %d, want %d", n, c.wantLookups)
			}
			if c.wantLookups < 0 && n < 2 {
				t.Errorf("lookups = %d, want several: the import did not wait", n)
			}
			if diags.HasError() != c.wantError {
				t.Errorf("error diagnostics = %v, want an error: %v", diags.Errors(), c.wantError)
			}
			if got := diags.WarningsCount(); got != map[bool]int{true: 1, false: 0}[c.wantWarning] {
				t.Fatalf("warnings = %v, want a warning: %v", diags.Warnings(), c.wantWarning)
			}
			if c.wantWarning {
				detail := diags.Warnings()[0].Detail()
				for _, want := range []string{assertionURN, "datahub_freshness_assertion", "imported", "monitor_urn", "next refresh"} {
					if !strings.Contains(detail, want) {
						t.Errorf("warning detail does not mention %q:\n%s", want, detail)
					}
				}
			}
		})
	}
}
