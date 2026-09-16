// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"net/http"
)

// mockOrgDisplayPreferences holds the org-wide branding stored at
// globalSettingsInfo.visual on the globalSettings singleton.
//
// Empty string means "not set". DataHub offers no way to remove any of these
// fields once written, so the mock deliberately has no notion of absence for an
// individual field - matching the live behaviour the provider is written
// against.
type mockOrgDisplayPreferences struct {
	OrgName      string
	LogoURL      string
	PrimaryColor string
}

// handleUpdateOrganizationDisplayPreferences serves the
// updateOrganizationDisplayPreferences GraphQL mutation.
//
// Merge semantics mirror DataHub Cloud, verified live: the mutation is a
// per-field read-modify-write. A field absent from the input is left at its
// current value, and an explicit null is ignored rather than clearing the
// field. Only an empty string resets a field.
//
// The provider always sends customOrgName and customLogoUrl, so for those two
// the fidelity is not guarding a specific provider bug today. It is modelled
// anyway so the mock does not quietly diverge from live: if the client is ever
// changed to send only the changed field, tests will keep matching real
// behaviour instead of passing against a more forgiving stub.
//
// primaryColor is different: the provider deliberately omits it while the
// attribute has never been configured, because the field does not exist in the
// input type before DataHub Cloud v2.2.0 and GraphQL would reject the whole
// mutation over it. Merge fidelity is therefore load-bearing here - a mock that
// blanked an unmentioned primaryColor would make the omission look like a
// reset and hide the distinction the resource is built around.
func (s *mockServer) handleUpdateOrganizationDisplayPreferences(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)

	s.mu.Lock()
	if raw, present := input["customOrgName"]; present {
		if v, ok := raw.(string); ok {
			s.orgDisplayPreferences.OrgName = v
		}
	}
	if raw, present := input["customLogoUrl"]; present {
		if v, ok := raw.(string); ok {
			s.orgDisplayPreferences.LogoURL = v
		}
	}
	if raw, present := input["primaryColor"]; present {
		if v, ok := raw.(string); ok {
			s.orgDisplayPreferences.PrimaryColor = v
		}
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"updateOrganizationDisplayPreferences": true},
	})
}

// handleGlobalSettingsItem serves
// GET /openapi/v3/entity/globalsettings/{urn}, the strongly-consistent read
// path the provider uses for the singleton.
//
// The singleton always exists. Sibling sections of globalSettingsInfo are
// included with fixed values so a test can assert the provider never disturbs
// them, and so the response shape matches a live instance rather than being
// narrowed to just the fields under test.
func (s *mockServer) handleGlobalSettingsItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	prefs := s.orgDisplayPreferences
	s.mu.Unlock()

	visual := map[string]any{
		"customOrgName": prefs.OrgName,
		"customLogoUrl": prefs.LogoURL,
		"primaryColor":  prefs.PrimaryColor,
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"urn": globalSettingsMockURN,
		"globalSettingsKey": map[string]any{
			"value": map[string]any{"id": "0"},
		},
		"globalSettingsInfo": map[string]any{
			"value": map[string]any{
				"visual": visual,
				// Unmanaged sections, present so the shape matches live and so
				// the provider's read is exercised against a realistic payload.
				"docPropagation": map[string]any{
					"enabled":                  true,
					"columnPropagationEnabled": true,
				},
				"homePage": map[string]any{
					"defaultTemplate": "urn:li:dataHubPageTemplate:home_default_1",
				},
				"views": map[string]any{},
			},
		},
	})
}

// globalSettingsMockURN is the singleton URN the mock serves.
const globalSettingsMockURN = "urn:li:globalSettings:0"
