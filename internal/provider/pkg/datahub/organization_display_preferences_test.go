// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestIsOrganizationDisplayPreferencesCloudOnlyError covers the OSS detection
// heuristic. The real signal is a GraphQL validation error for a mutation field
// that does not exist in the OSS schema.
func TestIsOrganizationDisplayPreferencesCloudOnlyError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		msg  string
		want bool
	}{
		{
			name: "OSS missing mutation",
			msg:  "Validation error of type FieldUndefined: Field 'updateOrganizationDisplayPreferences' in type 'Mutation' is undefined",
			want: true,
		},
		{
			name: "undefined field on a different type is not our signal",
			msg:  "Validation error of type FieldUndefined: Field 'somethingElse' in type 'Query' is undefined",
			want: false,
		},
		{
			name: "authorization failure is a real error, not a Cloud-only signal",
			msg:  "Unauthorized to perform this action. Please contact your DataHub administrator.",
			want: false,
		},
		{
			name: "empty",
			msg:  "",
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isOrganizationDisplayPreferencesCloudOnlyError(tc.msg); got != tc.want {
				t.Errorf("isOrganizationDisplayPreferencesCloudOnlyError(%q) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}

// TestOrganizationDisplayPreferencesCloudOnlyErrorSurvivesWrapping guards the
// resource's diagnostic path: the resource wraps the client error before
// inspecting it with errors.Is, so a sentinel that did not unwrap cleanly would
// silently downgrade the "requires DataHub Cloud" diagnostic to a raw GraphQL
// error. Only the nightly OSS job exercises that path end to end, so assert the
// unwrapping here.
func TestOrganizationDisplayPreferencesCloudOnlyErrorSurvivesWrapping(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("writing organization display preferences: %w", ErrOrganizationDisplayPreferencesCloudOnly)
	if !errors.Is(wrapped, ErrOrganizationDisplayPreferencesCloudOnly) {
		t.Fatal("wrapped Cloud-only sentinel no longer matches errors.Is; the resource would emit a raw API error instead of the Cloud-only diagnostic")
	}

	other := fmt.Errorf("writing organization display preferences: %w", errors.New("boom"))
	if errors.Is(other, ErrOrganizationDisplayPreferencesCloudOnly) {
		t.Fatal("unrelated error matched the Cloud-only sentinel")
	}
}

// globalSettingsResponse builds an OpenAPI v3 entity response for the
// globalSettings singleton with the given visual section.
func globalSettingsResponse(orgName, logoURL string) map[string]any {
	return map[string]any{
		"urn": GlobalSettingsURN,
		"globalSettingsInfo": map[string]any{
			"value": map[string]any{
				"visual": map[string]any{
					"customOrgName": orgName,
					"customLogoUrl": logoURL,
				},
			},
		},
	}
}

func TestGetOrganizationDisplayPreferences(t *testing.T) {
	t.Run("reads_the_visual_section", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(globalSettingsResponse("Acme Data", "https://acme.example/logo.png"))
		}))
		defer srv.Close()

		got, found, err := newTestClient(t, srv).GetOrganizationDisplayPreferences(t.Context())
		if err != nil {
			t.Fatalf("GetOrganizationDisplayPreferences() error = %v", err)
		}
		if !found {
			t.Fatal("found = false, want true")
		}
		if got.OrgName != "Acme Data" || got.LogoURL != "https://acme.example/logo.png" {
			t.Errorf("got %+v, want the visual section decoded", got)
		}
		// The lowercase entity path segment is easy to get wrong and fails at
		// runtime only.
		if !strings.Contains(gotPath, "/openapi/v3/entity/globalsettings/") {
			t.Errorf("request path = %q, want the lowercase globalsettings entity path", gotPath)
		}
	})

	t.Run("absent_visual_section_reads_as_unset", func(t *testing.T) {
		// A fresh instance has no visual section at all; that must read as
		// empty rather than erroring or nil-panicking.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"urn":                GlobalSettingsURN,
				"globalSettingsInfo": map[string]any{"value": map[string]any{}},
			})
		}))
		defer srv.Close()

		got, found, err := newTestClient(t, srv).GetOrganizationDisplayPreferences(t.Context())
		if err != nil || !found {
			t.Fatalf("error = %v, found = %v; want nil, true", err, found)
		}
		if got.OrgName != "" || got.LogoURL != "" {
			t.Errorf("got %+v, want both fields empty", got)
		}
	})

	t.Run("not_found_reports_absent_without_error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		_, found, err := newTestClient(t, srv).GetOrganizationDisplayPreferences(t.Context())
		if err != nil {
			t.Fatalf("error = %v, want nil for a 404", err)
		}
		if found {
			t.Error("found = true, want false for a 404")
		}
	})

	t.Run("server_error_surfaces_status_and_body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("no privilege"))
		}))
		defer srv.Close()

		_, _, err := newTestClient(t, srv).GetOrganizationDisplayPreferences(t.Context())
		if err == nil {
			t.Fatal("error = nil, want an error for HTTP 403")
		}
		if !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "no privilege") {
			t.Errorf("error = %v, want it to include the status and response body", err)
		}
	})
}

func TestSetOrganizationDisplayPreferences(t *testing.T) {
	// writeThenReadHandler serves the mutation and then the read-back, echoing
	// whatever readBack says the server now holds.
	writeThenReadHandler := func(readBack func() (string, string)) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/globalsettings/") {
				orgName, logoURL := readBack()
				_ = json.NewEncoder(w).Encode(globalSettingsResponse(orgName, logoURL))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"updateOrganizationDisplayPreferences": true},
			})
		})
	}

	t.Run("success_when_values_persist", func(t *testing.T) {
		srv := httptest.NewServer(writeThenReadHandler(func() (string, string) {
			return "Acme Data", "https://acme.example/logo.png"
		}))
		defer srv.Close()

		err := newTestClient(t, srv).SetOrganizationDisplayPreferences(t.Context(),
			OrganizationDisplayPreferences{OrgName: "Acme Data", LogoURL: "https://acme.example/logo.png"})
		if err != nil {
			t.Fatalf("SetOrganizationDisplayPreferences() error = %v", err)
		}
	})

	t.Run("read_back_guard_fires_when_values_do_not_persist", func(t *testing.T) {
		// The silent-no-op guard: DataHub can return success while dropping the
		// write. A guard that does not actually fire is worse than none, so
		// assert it does.
		srv := httptest.NewServer(writeThenReadHandler(func() (string, string) {
			return "", "" // server claims success but stored nothing
		}))
		defer srv.Close()

		err := newTestClient(t, srv).SetOrganizationDisplayPreferences(t.Context(),
			OrganizationDisplayPreferences{OrgName: "Acme Data"})
		if err == nil {
			t.Fatal("error = nil, want the read-back verification to fail")
		}
		if !strings.Contains(err.Error(), "did not persist") {
			t.Errorf("error = %v, want it to report the values did not persist", err)
		}
	})

	t.Run("oss_missing_mutation_maps_to_cloud_only_sentinel", func(t *testing.T) {
		srv := httptest.NewServer(ossGraphQLHandler("updateOrganizationDisplayPreferences"))
		defer srv.Close()

		err := newTestClient(t, srv).SetOrganizationDisplayPreferences(t.Context(),
			OrganizationDisplayPreferences{OrgName: "Acme Data"})
		if !errors.Is(err, ErrOrganizationDisplayPreferencesCloudOnly) {
			t.Fatalf("error = %v, want ErrOrganizationDisplayPreferencesCloudOnly", err)
		}
	})

	t.Run("privilege_denial_surfaces_verbatim", func(t *testing.T) {
		// Not a Cloud-only signal: the caller lacks
		// MANAGE_ORGANIZATION_DISPLAY_PREFERENCES. Misclassifying this would
		// tell a Cloud user to remove a resource that is perfectly valid.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]any{
					{"message": "Unauthorized to perform this action. Please contact your DataHub administrator."},
				},
			})
		}))
		defer srv.Close()

		err := newTestClient(t, srv).SetOrganizationDisplayPreferences(t.Context(),
			OrganizationDisplayPreferences{OrgName: "Acme Data"})
		if err == nil {
			t.Fatal("error = nil, want the authorization error surfaced")
		}
		if errors.Is(err, ErrOrganizationDisplayPreferencesCloudOnly) {
			t.Fatal("an authorization failure was misclassified as Cloud-only")
		}
		if !strings.Contains(err.Error(), "Unauthorized") {
			t.Errorf("error = %v, want the server message preserved", err)
		}
	})
}
