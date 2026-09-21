// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// Scenario builders for datahub_organization_display_preferences, the
// globalSettings singleton's org branding.
//
// Singleton caveat for live runs: these settings are global to the instance, so
// unlike every other scenario in this package the values cannot be namespaced
// with tg.Name(). A live run therefore mutates real org branding and the final
// step resets it. Only one such scenario should run against a given instance at
// a time.

const orgDisplayPrefsAddr = "datahub_organization_display_preferences.test"

// orgDisplayPrefsConfig builds a config with the given attributes. A nil value
// omits the attribute entirely, which is the "reset to DataHub default" case
// for org_name and logo_url, and for primary_color is a reset only once the
// attribute has been set at least once (see primaryColorToWrite).
func orgDisplayPrefsConfig(orgName, logoURL, primaryColor *string) string {
	body := ""
	if orgName != nil {
		body += fmt.Sprintf("\n  org_name = %q", *orgName)
	}
	if logoURL != nil {
		body += fmt.Sprintf("\n  logo_url = %q", *logoURL)
	}
	if primaryColor != nil {
		body += fmt.Sprintf("\n  primary_color = %q", *primaryColor)
	}
	return providerBlock + fmt.Sprintf(`
resource "datahub_organization_display_preferences" "test" {%s
}
`, body)
}

func strPtr(s string) *string { return &s }

// OrganizationDisplayPreferencesLifecycleSteps covers the singleton lifecycle:
// set both fields, change one in place, drop a field (which resets it rather
// than leaving it stale), plan idempotency, and import.
func OrganizationDisplayPreferencesLifecycleSteps() []resource.TestStep {
	const urn = "urn:li:globalSettings:0"

	return []resource.TestStep{
		{
			// All three fields set. There is no "create" server-side; this is
			// an update of an always-present singleton.
			Config: orgDisplayPrefsConfig(strPtr("TF Example Org"), strPtr("https://example.com/tf-example-logo.png"), strPtr("#EC0016")),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(orgDisplayPrefsAddr, tfjsonpath.New("urn"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(orgDisplayPrefsAddr, tfjsonpath.New("id"), knownvalue.StringExact(urn)),
				statecheck.ExpectKnownValue(orgDisplayPrefsAddr, tfjsonpath.New("org_name"), knownvalue.StringExact("TF Example Org")),
				statecheck.ExpectKnownValue(orgDisplayPrefsAddr, tfjsonpath.New("logo_url"), knownvalue.StringExact("https://example.com/tf-example-logo.png")),
				statecheck.ExpectKnownValue(orgDisplayPrefsAddr, tfjsonpath.New("primary_color"), knownvalue.StringExact("#EC0016")),
			},
		},
		{
			// Re-applying the same config must be a no-op.
			Config:   orgDisplayPrefsConfig(strPtr("TF Example Org"), strPtr("https://example.com/tf-example-logo.png"), strPtr("#EC0016")),
			PlanOnly: true,
		},
		{
			// In-place update: the singleton is updated, never replaced, and
			// both fields round-trip. (The provider always sends both fields,
			// so this does not exercise DataHub's per-field merge - see the
			// mock handler's note on why it models merge faithfully anyway.)
			Config: orgDisplayPrefsConfig(strPtr("TF Example Org Renamed"), strPtr("https://example.com/tf-example-logo.png"), strPtr("#0B6E4F")),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(orgDisplayPrefsAddr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(orgDisplayPrefsAddr, tfjsonpath.New("org_name"), knownvalue.StringExact("TF Example Org Renamed")),
				statecheck.ExpectKnownValue(orgDisplayPrefsAddr, tfjsonpath.New("logo_url"), knownvalue.StringExact("https://example.com/tf-example-logo.png")),
				statecheck.ExpectKnownValue(orgDisplayPrefsAddr, tfjsonpath.New("primary_color"), knownvalue.StringExact("#0B6E4F")),
			},
		},
		{
			// Import while managed. Any id is accepted; the URN is fixed.
			ResourceName:      orgDisplayPrefsAddr,
			ImportState:       true,
			ImportStateId:     urn,
			ImportStateVerify: true,
		},
		{
			// Dropping logo_url and primary_color resets both: the provider
			// owns every field it exposes, so an omitted attribute is not
			// "leave it alone". primary_color reaches that state the moment it
			// has been set once, which it has by now - the never-set case is
			// covered by OrganizationDisplayPreferencesUnmanagedPrimaryColorSteps.
			//
			// The data source is what distinguishes a reset from a state-only
			// forgetting. Asserting null in state alone would pass just as
			// happily against a provider that dropped the field from the write
			// and left the instance showing #0B6E4F, which is the exact bug the
			// clear-on-withdrawal branch exists to prevent.
			Config: providerBlock + `
resource "datahub_organization_display_preferences" "test" {
  org_name = "TF Example Org Renamed"
}

data "datahub_organization_display_preferences" "after_drop" {
  depends_on = [datahub_organization_display_preferences.test]
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(orgDisplayPrefsAddr, tfjsonpath.New("org_name"), knownvalue.StringExact("TF Example Org Renamed")),
				statecheck.ExpectKnownValue(orgDisplayPrefsAddr, tfjsonpath.New("logo_url"), knownvalue.Null()),
				statecheck.ExpectKnownValue(orgDisplayPrefsAddr, tfjsonpath.New("primary_color"), knownvalue.Null()),
				statecheck.ExpectKnownValue(
					"data.datahub_organization_display_preferences.after_drop",
					tfjsonpath.New("primary_color"), knownvalue.Null()),
			},
		},
		{
			// Reset all three before the framework's destroy so a live instance
			// is left with default branding rather than test values.
			Config: orgDisplayPrefsConfig(strPtr(""), strPtr(""), strPtr("")),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(orgDisplayPrefsAddr, tfjsonpath.New("org_name"), knownvalue.StringExact("")),
				statecheck.ExpectKnownValue(orgDisplayPrefsAddr, tfjsonpath.New("logo_url"), knownvalue.StringExact("")),
				statecheck.ExpectKnownValue(orgDisplayPrefsAddr, tfjsonpath.New("primary_color"), knownvalue.StringExact("")),
			},
		},
	}
}

// OrganizationDisplayPreferencesDataSourceSteps proves the data source reads
// the same singleton the resource writes, without managing it.
func OrganizationDisplayPreferencesDataSourceSteps() []resource.TestStep {
	const dsAddr = "data.datahub_organization_display_preferences.test"

	cfg := providerBlock + `
resource "datahub_organization_display_preferences" "test" {
  org_name      = "TF Example DS Org"
  logo_url      = "https://example.com/tf-example-ds-logo.png"
  primary_color = "#EC0016"
}

data "datahub_organization_display_preferences" "test" {
  depends_on = [datahub_organization_display_preferences.test]
}
`

	return []resource.TestStep{
		{
			Config: cfg,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(dsAddr, tfjsonpath.New("org_name"), knownvalue.StringExact("TF Example DS Org")),
				statecheck.ExpectKnownValue(dsAddr, tfjsonpath.New("logo_url"), knownvalue.StringExact("https://example.com/tf-example-ds-logo.png")),
				statecheck.ExpectKnownValue(dsAddr, tfjsonpath.New("primary_color"), knownvalue.StringExact("#EC0016")),
				statecheck.ExpectKnownValue(dsAddr, tfjsonpath.New("urn"), knownvalue.StringExact("urn:li:globalSettings:0")),
			},
		},
		{
			// Reset before destroy (see the singleton caveat above).
			Config: orgDisplayPrefsConfig(strPtr(""), strPtr(""), strPtr("")),
		},
	}
}

// OrganizationDisplayPreferencesExternalEditSteps proves the provider owns the
// values: a change made outside Terraform shows as drift and is corrected on
// the next apply. It drives the GraphQL mutation directly to stand in for
// someone editing the field in the DataHub UI.
func OrganizationDisplayPreferencesExternalEditSteps() []resource.TestStep {
	simulateEdit := func() {
		url := os.Getenv("DATAHUB_GMS_URL") + "/api/graphql"
		body := `{"query":"mutation updateOrganizationDisplayPreferences($input: UpdateOrganizationDisplayPreferencesInput!) { updateOrganizationDisplayPreferences(input: $input) }",` +
			`"variables":{"input":{"customOrgName":"Renamed In The UI"}}}`
		resp, err := http.Post(url, "application/json", strings.NewReader(body)) //nolint:noctx
		if err != nil {
			panic(fmt.Sprintf("OrganizationDisplayPreferencesExternalEditSteps PreConfig: POST mutation: %v", err))
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode >= 300 {
			panic(fmt.Sprintf("OrganizationDisplayPreferencesExternalEditSteps PreConfig: unexpected status %d", resp.StatusCode))
		}
	}

	return []resource.TestStep{
		{
			Config: orgDisplayPrefsConfig(strPtr("TF Example Owned"), nil, nil),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(orgDisplayPrefsAddr, tfjsonpath.New("org_name"), knownvalue.StringExact("TF Example Owned")),
			},
		},
		{
			// Simulate someone renaming the org in the DataHub UI, then assert
			// the next plan is non-empty (drift detected) and apply restores it.
			PreConfig: simulateEdit,
			Config:    orgDisplayPrefsConfig(strPtr("TF Example Owned"), nil, nil),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(orgDisplayPrefsAddr, plancheck.ResourceActionUpdate),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(orgDisplayPrefsAddr, tfjsonpath.New("org_name"), knownvalue.StringExact("TF Example Owned")),
			},
		},
		{
			Config: orgDisplayPrefsConfig(strPtr(""), strPtr(""), strPtr("")),
		},
	}
}

// OrganizationDisplayPreferencesUnmanagedPrimaryColorSteps proves the one place
// primary_color departs from its siblings: while the attribute has never been
// set, the resource neither adopts nor clears the instance's brand colour.
//
// That is what makes the attribute additive rather than a breaking change. The
// same omission is what keeps the mutation free of a field that DataHub Cloud
// instances older than v2.2.0 would reject outright, and the two are the same
// code path - so a regression that started blanking an undeclared colour would
// also be a regression that starts failing every apply on an older Cloud.
//
// Mock-only for the same reason as the external-edit scenario: it drives the
// mutation out of band.
func OrganizationDisplayPreferencesUnmanagedPrimaryColorSteps() []resource.TestStep {
	const dsAddr = "data.datahub_organization_display_preferences.unmanaged"

	setColorOutOfBand := func() {
		url := os.Getenv("DATAHUB_GMS_URL") + "/api/graphql"
		body := `{"query":"mutation updateOrganizationDisplayPreferences($input: UpdateOrganizationDisplayPreferencesInput!) { updateOrganizationDisplayPreferences(input: $input) }",` +
			`"variables":{"input":{"primaryColor":"#123456"}}}`
		resp, err := http.Post(url, "application/json", strings.NewReader(body)) //nolint:noctx
		if err != nil {
			panic(fmt.Sprintf("OrganizationDisplayPreferencesUnmanagedPrimaryColorSteps PreConfig: POST mutation: %v", err))
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode >= 300 {
			panic(fmt.Sprintf("OrganizationDisplayPreferencesUnmanagedPrimaryColorSteps PreConfig: unexpected status %d", resp.StatusCode))
		}
	}

	cfg := providerBlock + `
resource "datahub_organization_display_preferences" "test" {
  org_name = "TF Example Unmanaged Colour"
}

data "datahub_organization_display_preferences" "unmanaged" {
  depends_on = [datahub_organization_display_preferences.test]
}
`

	return []resource.TestStep{
		{
			// Someone sets a brand colour in the DataHub UI, then Terraform
			// applies a configuration that says nothing about primary_color.
			PreConfig: setColorOutOfBand,
			Config:    cfg,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(orgDisplayPrefsAddr, tfjsonpath.New("org_name"), knownvalue.StringExact("TF Example Unmanaged Colour")),
				// Not adopted: an undeclared value is not this resource's, and
				// adopting it would show as drift and be cleared next apply.
				statecheck.ExpectKnownValue(orgDisplayPrefsAddr, tfjsonpath.New("primary_color"), knownvalue.Null()),
				// Not cleared either: the instance still has what the UI set.
				statecheck.ExpectKnownValue(dsAddr, tfjsonpath.New("primary_color"), knownvalue.StringExact("#123456")),
			},
		},
		{
			// A second apply must not drift into ownership either: an
			// unmanaged value stays unmanaged for as long as it is undeclared.
			Config:   cfg,
			PlanOnly: true,
		},
		{
			Config: orgDisplayPrefsConfig(strPtr(""), strPtr(""), strPtr("")),
		},
	}
}
