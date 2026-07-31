// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"strings"
	"testing"
)

// NewClient quietly normalises its host and token, and every API call in the
// provider depends on the result. None of that behaviour was asserted: the
// existing tests construct a client and exercise endpoints, so a change to the
// normalisation would surface as unrelated request failures rather than as a
// failing test here. These pin each documented behaviour to a case.
//
// This matters more since the credential resolution changed: with the
// ~/.datahubenv fallback removed, whatever the configuration or environment
// supplies now reaches NewClient directly, and users hand it a wider variety of
// shapes than a CLI-written file did.

func TestNewClientNormalisesHost(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		host        string
		wantBaseURL string
	}{
		{"plain https host", "https://datahub.example.com", "https://datahub.example.com"},
		{"http is preserved, not upgraded", "http://localhost:8080", "http://localhost:8080"},

		// A host with no scheme defaults to https rather than being rejected.
		{"scheme-less host defaults to https", "datahub.example.com", "https://datahub.example.com"},
		{"protocol-relative host defaults to https", "//datahub.example.com", "https://datahub.example.com"},

		// Surrounding whitespace is tolerated: values arriving via a shell
		// export or a heredoc frequently carry it.
		{"leading and trailing whitespace trimmed", "  https://datahub.example.com  ", "https://datahub.example.com"},

		{"trailing slash removed", "https://datahub.example.com/", "https://datahub.example.com"},

		// The /gms suffix is what the DataHub CLI writes and what operators
		// copy from it, but every API path is rooted at /.
		{"gms suffix stripped", "https://datahub.example.com/gms", "https://datahub.example.com"},
		{"gms suffix with trailing slash stripped", "https://datahub.example.com/gms/", "https://datahub.example.com"},
		{"gms suffix stripped with a port", "http://localhost:8080/gms", "http://localhost:8080"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := NewClient(tc.host, "test-token")
			if err != nil {
				t.Fatalf("NewClient(%q) error = %v", tc.host, err)
			}
			if got := c.BaseURL(); got != tc.wantBaseURL {
				t.Errorf("BaseURL() = %q, want %q", got, tc.wantBaseURL)
			}
		})
	}
}

func TestNewClientRejectsUnusableInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		host    string
		token   string
		wantErr string
	}{
		{"empty host", "", "test-token", "host is required"},
		{"whitespace-only host", "   ", "test-token", "host is required"},
		{"host with no hostname", "https://", "test-token", "missing host"},
		{"empty token", "https://datahub.example.com", "", "token is required"},
		{"whitespace-only token", "https://datahub.example.com", "  ", "token is required"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := NewClient(tc.host, tc.token)
			if err == nil {
				t.Fatalf("NewClient(%q, %q) error = nil, want %q", tc.host, tc.token, tc.wantErr)
			}
			if c != nil {
				t.Error("a failed NewClient must not return a client")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestNewClientBuildsAuthHeaderOnce(t *testing.T) {
	t.Parallel()

	// The token is sent as an Authorization bearer header. An operator who
	// copies a token out of a curl example or an HTTP log brings the "Bearer "
	// prefix with it, and doubling it produces a 401 that says nothing useful.
	cases := []struct {
		name  string
		token string
	}{
		{"bare token gains the prefix", "abc123"},
		{"prefixed token is not doubled", "Bearer abc123"},
		{"prefix match is case-insensitive", "bearer abc123"},
		{"surrounding whitespace trimmed first", "  abc123  "},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := NewClient("https://datahub.example.com", tc.token)
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			got := c.authHeader
			if strings.Count(strings.ToLower(got), "bearer ") != 1 {
				t.Errorf("authHeader = %q, want exactly one bearer prefix", got)
			}
			if !strings.HasSuffix(got, "abc123") {
				t.Errorf("authHeader = %q, want it to end with the token", got)
			}
		})
	}
}
