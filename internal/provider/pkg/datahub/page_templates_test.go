// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetDefaultHomePageTemplateURN covers the read that guards a destroy of
// the organisation's default home page template.
//
// Each case matters for a different reason, so they are not merely permutations:
// a populated pointer must be recognised (or the guard never fires), an absent
// pointer or missing aspect must read as "" rather than erroring (or the guard
// blocks deletes on instances that simply have no default set), and a server
// error must surface as an error so the caller can decide -- the resource turns
// that into a warning and proceeds rather than making itself undeletable.
func TestGetDefaultHomePageTemplateURN(t *testing.T) {
	t.Parallel()

	const wantURN = "urn:li:dataHubPageTemplate:home_default_1"

	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr bool
	}{
		{
			name:   "pointer set",
			status: http.StatusOK,
			body: `{"urn":"urn:li:globalSettings:0",
			        "globalSettingsInfo":{"value":{
			          "homePage":{"defaultTemplate":"` + wantURN + `"},
			          "docPropagation":{"enabled":true}}}}`,
			want: wantURN,
		},
		{
			name:   "homePage section absent",
			status: http.StatusOK,
			body:   `{"urn":"urn:li:globalSettings:0","globalSettingsInfo":{"value":{"views":{}}}}`,
			want:   "",
		},
		{
			name:   "aspect absent entirely",
			status: http.StatusOK,
			body:   `{"urn":"urn:li:globalSettings:0"}`,
			want:   "",
		},
		{
			// The singleton always exists on a real instance, but a 404 must not
			// be an error: it means "no default to protect", not "read failed".
			name:   "settings singleton missing",
			status: http.StatusNotFound,
			body:   `{}`,
			want:   "",
		},
		{
			name:    "server error",
			status:  http.StatusInternalServerError,
			body:    `{"error":"boom"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got, want := r.URL.Path, "/openapi/v3/entity/globalsettings/urn:li:globalSettings:0"; got != want {
					t.Errorf("unexpected path %q, want %q", got, want)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			c, err := NewClient(srv.URL, "test-token")
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}

			got, err := c.GetDefaultHomePageTemplateURN(context.Background())
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
