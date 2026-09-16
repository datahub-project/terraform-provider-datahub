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
			// Verbatim from the nightly OSS Quickstart job. Variable-type
			// validation rejects the document before field resolution, so the
			// mutation never reports as undefined and matching FieldUndefined
			// alone let a raw GraphQL error reach the user.
			name: "OSS missing input type",
			msg:  "Validation error (UnknownType) : Unknown type 'UpdateOrganizationDisplayPreferencesInput'",
			want: true,
		},
		{
			name: "unknown type unrelated to this resource is not our signal",
			msg:  "Validation error (UnknownType) : Unknown type 'SomeOtherInput'",
			want: false,
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
	return globalSettingsResponseWithColor(orgName, logoURL, "")
}

// globalSettingsResponseWithColor is globalSettingsResponse plus the brand
// colour DataHub Cloud added in v2.2.0.
func globalSettingsResponseWithColor(orgName, logoURL, primaryColor string) map[string]any {
	return map[string]any{
		"urn": GlobalSettingsURN,
		"globalSettingsInfo": map[string]any{
			"value": map[string]any{
				"visual": map[string]any{
					"customOrgName": orgName,
					"customLogoUrl": logoURL,
					"primaryColor":  primaryColor,
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

	t.Run("reads_the_brand_colour", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(globalSettingsResponseWithColor("Acme Data", "", "#EC0016"))
		}))
		defer srv.Close()

		got, _, err := newTestClient(t, srv).GetOrganizationDisplayPreferences(t.Context())
		if err != nil {
			t.Fatalf("GetOrganizationDisplayPreferences() error = %v", err)
		}
		if got.PrimaryColor != "#EC0016" {
			t.Errorf("PrimaryColor = %q, want %q", got.PrimaryColor, "#EC0016")
		}
	})

	t.Run("absent_brand_colour_reads_as_unset", func(t *testing.T) {
		// What an instance predating DataHub Cloud v2.2.0 returns: the visual
		// section is there, primaryColor simply is not. The read path is
		// OpenAPI v3 JSON rather than a GraphQL selection set, so an absent
		// field is merely absent - no error, no failed query, nothing for the
		// user to notice. That is the whole reason Read needs no version
		// handling.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"urn": GlobalSettingsURN,
				"globalSettingsInfo": map[string]any{
					"value": map[string]any{
						"visual": map[string]any{
							"customOrgName": "Acme Data",
							"customLogoUrl": "",
						},
					},
				},
			})
		}))
		defer srv.Close()

		got, found, err := newTestClient(t, srv).GetOrganizationDisplayPreferences(t.Context())
		if err != nil || !found {
			t.Fatalf("error = %v, found = %v; want nil, true", err, found)
		}
		if got.PrimaryColor != "" {
			t.Errorf("PrimaryColor = %q, want empty for a server that has no such field", got.PrimaryColor)
		}
		if got.OrgName != "Acme Data" {
			t.Errorf("OrgName = %q, want the rest of the section still decoded", got.OrgName)
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
			OrganizationDisplayPreferencesUpdate{OrgName: "Acme Data", LogoURL: "https://acme.example/logo.png"})
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
			OrganizationDisplayPreferencesUpdate{OrgName: "Acme Data"})
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
			OrganizationDisplayPreferencesUpdate{OrgName: "Acme Data"})
		if !errors.Is(err, ErrOrganizationDisplayPreferencesCloudOnly) {
			t.Fatalf("error = %v, want ErrOrganizationDisplayPreferencesCloudOnly", err)
		}
	})

	t.Run("oss_missing_input_type_maps_to_cloud_only_sentinel", func(t *testing.T) {
		// The shape OSS Quickstart really returns. The mutation-level handler
		// above passed while this path emitted a raw GraphQL error, so the
		// nightly OSS job caught what the unit tests did not.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]any{
					{"message": "Validation error (UnknownType) : Unknown type 'UpdateOrganizationDisplayPreferencesInput'"},
				},
			})
		}))
		defer srv.Close()

		err := newTestClient(t, srv).SetOrganizationDisplayPreferences(t.Context(),
			OrganizationDisplayPreferencesUpdate{OrgName: "Acme Data"})
		if !errors.Is(err, ErrOrganizationDisplayPreferencesCloudOnly) {
			t.Fatalf("error = %v, want ErrOrganizationDisplayPreferencesCloudOnly", err)
		}
	})

	// captureInputHandler records the mutation's input variables, then serves
	// the read-back from whatever the caller says the server now holds.
	captureInputHandler := func(got *map[string]any, readBack func() (string, string, string)) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if strings.HasPrefix(r.URL.Path, "/openapi/v3/entity/globalsettings/") {
				orgName, logoURL, color := readBack()
				_ = json.NewEncoder(w).Encode(globalSettingsResponseWithColor(orgName, logoURL, color))
				return
			}
			var req struct {
				Variables struct {
					Input map[string]any `json:"input"`
				} `json:"variables"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decoding mutation body: %v", err)
			}
			*got = req.Variables.Input
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"updateOrganizationDisplayPreferences": true},
			})
		})
	}

	t.Run("primary_color_is_absent_from_the_input_when_not_requested", func(t *testing.T) {
		// The compatibility guarantee, asserted at the only place it is
		// observable. DataHub Cloud added primaryColor to
		// UpdateOrganizationDisplayPreferencesInput in v2.2.0, and GraphQL
		// coerces a variable's value against the input type before executing,
		// so an older server fails the entire mutation over a key it does not
		// define - whatever the value, including null. A test asserting only
		// that the *value* is empty would pass against a client that had
		// quietly reintroduced the key and broken every user on an older Cloud.
		var input map[string]any
		srv := httptest.NewServer(captureInputHandler(&input, func() (string, string, string) {
			return "Acme Data", "", "#123456"
		}))
		defer srv.Close()

		err := newTestClient(t, srv).SetOrganizationDisplayPreferences(t.Context(),
			OrganizationDisplayPreferencesUpdate{OrgName: "Acme Data"})
		if err != nil {
			t.Fatalf("SetOrganizationDisplayPreferences() error = %v", err)
		}
		if _, present := input["primaryColor"]; present {
			t.Errorf("input contains primaryColor = %#v; want the key absent entirely", input["primaryColor"])
		}
		// The other two are unconditional, so their absence would be a
		// different bug: the resource owns them outright.
		for _, k := range []string{"customOrgName", "customLogoUrl"} {
			if _, present := input[k]; !present {
				t.Errorf("input is missing %s; the resource owns that field unconditionally", k)
			}
		}
	})

	t.Run("read_back_ignores_a_colour_this_write_did_not_send", func(t *testing.T) {
		// Follows from the above: the server legitimately holds a colour set
		// elsewhere. Verifying it against a write that never mentioned it would
		// turn someone else's UI edit into a "values did not persist" failure
		// and make the resource unusable on any instance with a brand colour.
		var input map[string]any
		srv := httptest.NewServer(captureInputHandler(&input, func() (string, string, string) {
			return "Acme Data", "", "#123456"
		}))
		defer srv.Close()

		if err := newTestClient(t, srv).SetOrganizationDisplayPreferences(t.Context(),
			OrganizationDisplayPreferencesUpdate{OrgName: "Acme Data"}); err != nil {
			t.Fatalf("SetOrganizationDisplayPreferences() error = %v", err)
		}
	})

	t.Run("primary_color_is_sent_when_requested_including_empty", func(t *testing.T) {
		// "" is DataHub's clear-to-default sentinel, so it has to reach the
		// server as a value rather than being optimised away as "nothing to
		// say". If it were dropped, clearing a brand colour would be
		// impossible and destroy would silently leave it in place.
		for _, want := range []string{"#EC0016", ""} {
			var input map[string]any
			srv := httptest.NewServer(captureInputHandler(&input, func() (string, string, string) {
				return "", "", want
			}))

			color := want
			err := newTestClient(t, srv).SetOrganizationDisplayPreferences(t.Context(),
				OrganizationDisplayPreferencesUpdate{PrimaryColor: &color})
			srv.Close()

			if err != nil {
				t.Fatalf("SetOrganizationDisplayPreferences(%q) error = %v", want, err)
			}
			if got, present := input["primaryColor"]; !present || got != want {
				t.Errorf("input primaryColor = %#v (present=%v), want %q", got, present, want)
			}
		}
	})

	t.Run("read_back_guard_fires_when_the_colour_does_not_persist", func(t *testing.T) {
		// The silent-no-op guard, extended to the field it was not written
		// for. A server that accepts the mutation and stores nothing is the
		// failure this whole read-back exists to catch.
		var input map[string]any
		srv := httptest.NewServer(captureInputHandler(&input, func() (string, string, string) {
			return "", "", ""
		}))
		defer srv.Close()

		color := "#EC0016"
		err := newTestClient(t, srv).SetOrganizationDisplayPreferences(t.Context(),
			OrganizationDisplayPreferencesUpdate{PrimaryColor: &color})
		if err == nil {
			t.Fatal("error = nil, want the read-back verification to fail")
		}
		if !strings.Contains(err.Error(), "did not persist") {
			t.Errorf("error = %v, want it to report the value did not persist", err)
		}
	})

	t.Run("rejected_colour_maps_to_the_unsupported_sentinel_keeping_the_server_message", func(t *testing.T) {
		// What a pre-v2.2.0 Cloud does to a user who configures the attribute.
		// The exact wording has not been observed against such an instance, so
		// the detector keys only on the field name and the diagnostic carries
		// the server's message verbatim - a misfire still tells the user what
		// actually went wrong.
		const msg = "Variable 'input' has an invalid value: Field 'primaryColor' is not defined in the input type 'UpdateOrganizationDisplayPreferencesInput'"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]any{{"message": msg}},
			})
		}))
		defer srv.Close()

		color := "#EC0016"
		err := newTestClient(t, srv).SetOrganizationDisplayPreferences(t.Context(),
			OrganizationDisplayPreferencesUpdate{PrimaryColor: &color})
		if !errors.Is(err, ErrFieldUnsupportedByInstance) {
			t.Fatalf("error = %v, want ErrFieldUnsupportedByInstance", err)
		}
		if !strings.Contains(err.Error(), msg) {
			t.Errorf("error = %v, want the server message preserved verbatim", err)
		}
	})

	t.Run("a_write_without_a_colour_is_never_classified_as_unsupported", func(t *testing.T) {
		// The detector is deliberately loose, so it must not run at all when
		// the write carried no colour. Otherwise an unrelated server error that
		// happened to mention the field would tell a user to remove an
		// attribute they never set.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]any{{"message": "something about primaryColor went wrong"}},
			})
		}))
		defer srv.Close()

		err := newTestClient(t, srv).SetOrganizationDisplayPreferences(t.Context(),
			OrganizationDisplayPreferencesUpdate{OrgName: "Acme Data"})
		if errors.Is(err, ErrFieldUnsupportedByInstance) {
			t.Fatal("a write that sent no colour was classified as an unsupported-colour failure")
		}
		if err == nil || !strings.Contains(err.Error(), "primaryColor") {
			t.Errorf("error = %v, want the server message surfaced as an ordinary API error", err)
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
			OrganizationDisplayPreferencesUpdate{OrgName: "Acme Data"})
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
