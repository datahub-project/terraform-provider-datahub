// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
)

func TestWarnIfPlaintextEndpoint(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		host     string
		wantWarn bool
	}{
		{"plaintext to a remote host warns", "http://datahub.example.com", true},
		{"plaintext with a port warns", "http://datahub.example.com:8080", true},
		{"plaintext to a remote IP warns", "http://10.0.0.5:8080", true},
		{"https is fine", "https://datahub.example.com", false},
		{"scheme is matched case-insensitively", "HTTP://datahub.example.com", true},

		// Quickstart. The project's own Makefile uses http://localhost:8080, so
		// warning here would fire on the documented local workflow.
		{"localhost is exempt", "http://localhost:8080", false},
		{"127.0.0.1 is exempt", "http://127.0.0.1:8080", false},
		{"other 127/8 addresses are exempt", "http://127.0.0.2:8080", false},
		{"ipv6 loopback is exempt", "http://[::1]:8080", false},

		// A scheme-less host is upgraded to https by NewClient, so there is
		// nothing to warn about.
		{"scheme-less host does not warn", "datahub.example.com", false},

		{"empty host does not warn", "", false},
		{"unparseable host stays quiet, NewClient reports it", "http://[::1", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp := &provider.ConfigureResponse{}
			warnIfPlaintextEndpoint(resp, tc.host)

			gotWarn := resp.Diagnostics.WarningsCount() > 0
			if gotWarn != tc.wantWarn {
				t.Fatalf("warning = %v, want %v (diagnostics: %v)", gotWarn, tc.wantWarn, resp.Diagnostics)
			}
			if resp.Diagnostics.ErrorsCount() > 0 {
				t.Errorf("this function must never add an error, got: %v", resp.Diagnostics.Errors())
			}
			if tc.wantWarn {
				detail := resp.Diagnostics.Warnings()[0].Detail()
				for _, want := range []string{"cleartext", "https"} {
					if !strings.Contains(detail, want) {
						t.Errorf("warning detail missing %q: %s", want, detail)
					}
				}
			}
		})
	}
}

func TestIsLoopbackEndpoint(t *testing.T) {
	t.Parallel()

	for host, want := range map[string]bool{
		"localhost":           true,
		"LOCALHOST":           true,
		"127.0.0.1":           true,
		"127.255.255.254":     true,
		"::1":                 true,
		"datahub.example.com": false,
		"10.0.0.5":            false,
		// Not loopback despite the name resembling it; only the reserved
		// addresses and the literal name count.
		"localhost.example.com": false,
		"":                      false,
	} {
		if got := isLoopbackEndpoint(host); got != want {
			t.Errorf("isLoopbackEndpoint(%q) = %v, want %v", host, got, want)
		}
	}
}
