// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
