// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// regexpPersonalScope matches the diagnostic the provider raises for
// scope = "PERSONAL". Matching on the explanation rather than a generic
// "invalid" keeps the test honest: the point is that the user is told why the
// provider will not manage a per-user page, not merely that the apply failed.
var regexpPersonalScope = regexp.MustCompile(`PERSONAL scope is not supported`)

// PageModuleLifecycleSteps creates a link module, then updates it, checking the
// params round-trip.
//
// LINK rather than a parameterless type such as DOMAINS, because DataHub's
// upsertPageModule resolver will only create RICH_TEXT, LINK, ASSET_COLLECTION
// and HIERARCHY -- everything else is a module DataHub bootstraps and is
// rejected with "Attempted to create an unsupported module type". The mock
// accepted DOMAINS happily; only a live run caught it.
func PageModuleLifecycleSteps(moduleID string) []resource.TestStep {
	bare := providerBlock + fmt.Sprintf(`
resource "datahub_page_module" "test" {
  page_module_id = %q
  name           = "TF Test Module"
  type           = "LINK"

  params = {
    link = {
      link_url = "https://example.invalid/one"
    }
  }
}
`, moduleID)

	withParams := providerBlock + fmt.Sprintf(`
resource "datahub_page_module" "test" {
  page_module_id = %q
  name           = "TF Test Module Renamed"
  type           = "LINK"

  params = {
    link = {
      link_url    = "https://example.invalid/two"
      description = "updated"
    }
  }
}
`, moduleID)

	return []resource.TestStep{
		{
			Config: bare,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("datahub_page_module.test", "page_module_id", moduleID),
				resource.TestCheckResourceAttr("datahub_page_module.test", "type", "LINK"),
				resource.TestCheckResourceAttr("datahub_page_module.test", "scope", "GLOBAL"),
				resource.TestCheckResourceAttr("datahub_page_module.test", "urn",
					"urn:li:dataHubPageModule:"+moduleID),
				resource.TestCheckResourceAttr("datahub_page_module.test", "params.link.link_url",
					"https://example.invalid/one"),
				// An unset sibling within the same params block must read back
				// null rather than empty, or every plan shows a diff.
				resource.TestCheckNoResourceAttr("datahub_page_module.test", "params.link.description"),
			),
		},
		{
			Config: withParams,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("datahub_page_module.test", "name", "TF Test Module Renamed"),
				resource.TestCheckResourceAttr("datahub_page_module.test", "params.link.description", "updated"),
			),
		},
	}
}

// PageModuleRejectsPersonalScopeSteps asserts the provider refuses PERSONAL
// scope at plan time with an explanation, rather than writing a template owned
// by whichever account the token authenticates as.
func PageModuleRejectsPersonalScopeSteps(moduleID string) []resource.TestStep {
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_page_module" "test" {
  page_module_id = %q
  name           = "TF Test Personal"
  type           = "RICH_TEXT"
  scope          = "PERSONAL"

  params = {
    rich_text = {
      content = "x"
    }
  }
}
`, moduleID)

	return []resource.TestStep{
		{
			Config:      cfg,
			ExpectError: regexpPersonalScope,
			PlanOnly:    true,
		},
	}
}

// PageTemplateLifecycleSteps builds a two-row template over three modules, then
// reorders and drops one, which is what proves the resource owns the whole
// layout rather than merging into it.
func PageTemplateLifecycleSteps(prefix string) []resource.TestStep {
	modules := fmt.Sprintf(`
resource "datahub_page_module" "a" {
  page_module_id = "%[1]s-a"
  name           = "A"
  type           = "RICH_TEXT"

  params = {
    rich_text = {
      content = "A"
    }
  }
}

resource "datahub_page_module" "b" {
  page_module_id = "%[1]s-b"
  name           = "B"
  type           = "LINK"

  params = {
    link = {
      link_url = "https://example.invalid/b"
    }
  }
}

resource "datahub_page_module" "c" {
  page_module_id = "%[1]s-c"
  name           = "C"
  type           = "RICH_TEXT"

  params = {
    rich_text = {
      content = "C"
    }
  }
}
`, prefix)

	twoRows := providerBlock + modules + fmt.Sprintf(`
resource "datahub_page_template" "test" {
  page_template_id = "%s-tpl"

  rows = [
    { modules = [datahub_page_module.a.urn] },
    { modules = [datahub_page_module.b.urn, datahub_page_module.c.urn] },
  ]
}
`, prefix)

	oneRowReordered := providerBlock + modules + fmt.Sprintf(`
resource "datahub_page_template" "test" {
  page_template_id = "%s-tpl"

  rows = [
    { modules = [datahub_page_module.c.urn, datahub_page_module.a.urn] },
  ]
}
`, prefix)

	return []resource.TestStep{
		{
			Config: twoRows,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("datahub_page_template.test", "surface_type", "HOME_PAGE"),
				resource.TestCheckResourceAttr("datahub_page_template.test", "scope", "GLOBAL"),
				resource.TestCheckResourceAttr("datahub_page_template.test", "rows.#", "2"),
				resource.TestCheckResourceAttr("datahub_page_template.test", "rows.0.modules.#", "1"),
				resource.TestCheckResourceAttr("datahub_page_template.test", "rows.1.modules.#", "2"),
				resource.TestCheckResourceAttr("datahub_page_template.test", "rows.1.modules.0",
					"urn:li:dataHubPageModule:"+prefix+"-b"),
			),
		},
		{
			// Dropping a row and reversing the survivors must be reflected
			// exactly: order is significant and the resource replaces rather
			// than merges.
			Config: oneRowReordered,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("datahub_page_template.test", "rows.#", "1"),
				resource.TestCheckResourceAttr("datahub_page_template.test", "rows.0.modules.#", "2"),
				resource.TestCheckResourceAttr("datahub_page_template.test", "rows.0.modules.0",
					"urn:li:dataHubPageModule:"+prefix+"-c"),
				resource.TestCheckResourceAttr("datahub_page_template.test", "rows.0.modules.1",
					"urn:li:dataHubPageModule:"+prefix+"-a"),
			),
		},
	}
}

// PageTemplateAdoptionSteps covers adopting a template that already exists,
// which is the case that matters for the organisation's home page.
//
// The template is seeded by a direct aspect write in PreConfig rather than by a
// Terraform resource, because a resource removed from the configuration is
// destroyed rather than orphaned -- there would be nothing left to adopt. A raw
// write is also the more honest simulation: DataHub bootstraps home_default_1
// this way, with no Terraform involved.
//
// What is asserted is original_rows. The layout present at adoption must be
// captured, because destroy restores from it. A provider that captured its own
// layout instead would pass every other test here and quietly make the original
// unrecoverable.
func PageTemplateAdoptionSteps(prefix string) []resource.TestStep {
	templateID := prefix + "-adopt"

	adopted := providerBlock + fmt.Sprintf(`
resource "datahub_page_template" "adopter" {
  page_template_id = %q

  rows = [
    { modules = ["urn:li:dataHubPageModule:data_products"] },
  ]
}
`, templateID)

	return []resource.TestStep{
		{
			PreConfig: func() {
				seedPageTemplate(templateID, [][]string{
					{"urn:li:dataHubPageModule:your_assets"},
					{"urn:li:dataHubPageModule:top_domains", "urn:li:dataHubPageModule:platforms"},
				})
			},
			Config: adopted,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("datahub_page_template.adopter", "rows.#", "1"),
				resource.TestCheckResourceAttr("datahub_page_template.adopter", "rows.0.modules.0",
					"urn:li:dataHubPageModule:data_products"),
				resource.TestCheckResourceAttr("datahub_page_template.adopter", "original_rows.#", "2"),
				resource.TestCheckResourceAttr("datahub_page_template.adopter", "original_rows.0.modules.0",
					"urn:li:dataHubPageModule:your_assets"),
				resource.TestCheckResourceAttr("datahub_page_template.adopter", "original_rows.1.modules.1",
					"urn:li:dataHubPageModule:platforms"),
			),
		},
	}
}

// DeleteSeededPageTemplate removes a template seeded by seedPageTemplate.
//
// Needed because the adoption test leaves its subject behind by design: the
// provider restores an adopted template rather than deleting it, so Terraform's
// own teardown correctly does not remove it. Harmless on a disposable
// Quickstart, litter on a shared instance -- and this repository treats
// unattributable leftovers as a real cost.
func DeleteSeededPageTemplate(templateID string) error {
	body := fmt.Sprintf(`{"query":"mutation d($input: DeletePageTemplateInput!){ deletePageTemplate(input:$input) }",`+
		`"variables":{"input":{"urn":"urn:li:dataHubPageTemplate:%s"}}}`, templateID)

	req, err := http.NewRequest(http.MethodPost, os.Getenv("DATAHUB_GMS_URL")+"/api/graphql", strings.NewReader(body)) //nolint:noctx
	if err != nil {
		return fmt.Errorf("building the delete request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := os.Getenv("DATAHUB_GMS_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("deleting the seeded template: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return nil
}

// seedPageTemplate creates a page template outside Terraform, standing in for
// DataHub having bootstrapped it.
//
// Uses the GraphQL mutation rather than a raw aspect write only because it
// works identically against the mock and a live server; what matters for the
// test is that the entity exists before the provider's Create runs.
func seedPageTemplate(templateID string, rows [][]string) {
	rowsJSON := make([]string, 0, len(rows))
	for _, modules := range rows {
		quoted := make([]string, 0, len(modules))
		for _, m := range modules {
			quoted = append(quoted, fmt.Sprintf("%q", m))
		}
		rowsJSON = append(rowsJSON, fmt.Sprintf(`{"modules":[%s]}`, strings.Join(quoted, ",")))
	}

	body := fmt.Sprintf(`{"query":"mutation u($input: UpsertPageTemplateInput!){ upsertPageTemplate(input:$input){urn} }",`+
		`"variables":{"input":{"urn":"urn:li:dataHubPageTemplate:%s","scope":"GLOBAL","surfaceType":"HOME_PAGE",`+
		`"rows":[%s]}}}`, templateID, strings.Join(rowsJSON, ","))

	url := os.Getenv("DATAHUB_GMS_URL") + "/api/graphql"
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body)) //nolint:noctx
	if err != nil {
		panic(fmt.Sprintf("seedPageTemplate: build request: %v", err))
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := os.Getenv("DATAHUB_GMS_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(fmt.Sprintf("seedPageTemplate: POST: %v", err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		panic(fmt.Sprintf("seedPageTemplate: unexpected status %d", resp.StatusCode))
	}
}

// PageTemplateRowsFromVariableSteps drives rows from an input variable rather
// than a literal.
//
// This is the mandatory non-literal case for a resource with a nested
// attribute. What it currently proves is narrower than that framing suggests,
// and worth stating so nobody mistakes it for a guard it is not: the row object
// has a single Required attribute with no default, so Terraform plans nothing
// unknown inside it, and swapping the model to a Go slice was probed and did
// not break this step. Two earlier resources here shipped that bug, but this
// schema does not currently reach it.
//
// It stays for two reasons. Variable-driven rows are the realistic way an estate
// builds a page, so covering the path has value independent of unknowns. And the
// moment a row gains an Optional+Computed attribute the hazard becomes live and
// silent, at which point this step is what fails instead of a user's module.
func PageTemplateRowsFromVariableSteps(prefix string) []resource.TestStep {
	cfg := providerBlock + `
variable "rows" {
  type = list(object({
    modules = list(string)
  }))
}
` + fmt.Sprintf(`
resource "datahub_page_template" "test" {
  page_template_id = "%s-var"
  rows             = var.rows
}
`, prefix)

	return []resource.TestStep{
		{
			Config: cfg,
			// Bootstrapped module URNs, not invented ones. DataHub validates that
			// every module a template row references actually exists and rejects
			// the upsert otherwise -- fabricated URNs passed against the mock and
			// failed live. These eight ship with every instance.
			ConfigVariables: config.Variables{
				"rows": config.ListVariable(
					config.ObjectVariable(map[string]config.Variable{
						"modules": config.ListVariable(
							config.StringVariable("urn:li:dataHubPageModule:your_assets"),
						),
					}),
					config.ObjectVariable(map[string]config.Variable{
						"modules": config.ListVariable(
							config.StringVariable("urn:li:dataHubPageModule:top_domains"),
							config.StringVariable("urn:li:dataHubPageModule:platforms"),
						),
					}),
				),
			},
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("datahub_page_template.test", "rows.#", "2"),
				resource.TestCheckResourceAttr("datahub_page_template.test", "rows.1.modules.#", "2"),
			),
		},
	}
}

// HomePageSettingsDataSourceSteps reads the instance's default home-page
// template pointer.
//
// Asserting the id and the URN separately is the point: the id is what a
// configuration feeds to a datahub_page_template, so a wrong prefix-trim there
// would send users to build a template nobody sees.
func HomePageSettingsDataSourceSteps() []resource.TestStep {
	cfg := providerBlock + `
data "datahub_home_page_settings" "current" {}
`

	return []resource.TestStep{
		{
			Config: cfg,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.datahub_home_page_settings.current",
					"default_template_urn", "urn:li:dataHubPageTemplate:home_default_1"),
				resource.TestCheckResourceAttr("data.datahub_home_page_settings.current",
					"default_template_id", "home_default_1"),
			),
		},
	}
}

// PageTemplateEmptyRowsSteps covers a template with no rows at all. rows is
// Required and [PageTemplateRowInput!]! on the wire, so the provider sends an
// empty array rather than omitting the field; this asserts the server accepts
// that and the value round-trips as an empty list rather than null.
func PageTemplateEmptyRowsSteps(prefix string) []resource.TestStep {
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_page_template" "test" {
  page_template_id = "%s-empty"
  rows             = []
}
`, prefix)

	return []resource.TestStep{
		{
			Config: cfg,
			Check:  resource.TestCheckResourceAttr("datahub_page_template.test", "rows.#", "0"),
		},
	}
}
