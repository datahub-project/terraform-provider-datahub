// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
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

// PageTemplateImportSteps imports a template that exists outside Terraform.
//
// The assertion that matters is original_rows on the imported state. Import is
// the documented way to adopt the organisation's home page, and an import that
// failed to capture would leave the resource believing Terraform created the
// template -- so a later destroy would delete the page every user sees instead
// of restoring it.
func PageTemplateImportSteps(prefix string) []resource.TestStep {
	templateID := prefix + "-import"

	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_page_template" "imported" {
  page_template_id = %q

  rows = [
    { modules = ["urn:li:dataHubPageModule:your_assets"] },
  ]
}
`, templateID)

	return []resource.TestStep{
		{
			PreConfig: func() {
				seedPageTemplate(templateID, [][]string{
					{"urn:li:dataHubPageModule:top_domains", "urn:li:dataHubPageModule:platforms"},
				})
			},
			Config:             cfg,
			ResourceName:       "datahub_page_template.imported",
			ImportState:        true,
			ImportStateId:      templateID,
			ImportStatePersist: true,
			ImportStateCheck: func(states []*terraform.InstanceState) error {
				if len(states) != 1 {
					return fmt.Errorf("expected 1 imported state, got %d", len(states))
				}
				attrs := states[0].Attributes
				if got := attrs["original_rows.#"]; got != "1" {
					return fmt.Errorf("import did not capture original_rows (got %q); a destroy "+
						"would delete this template instead of restoring it", got)
				}
				if got := attrs["original_rows.0.modules.0"]; got != "urn:li:dataHubPageModule:top_domains" {
					return fmt.Errorf("original_rows captured the wrong layout: %q", got)
				}
				return nil
			},
		},
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

// PageModuleListParamsSteps exercises the two creatable module types whose
// params carry lists, which nothing else covers.
//
// ASSET_COLLECTION and HIERARCHY are the only creatable types with a list-valued
// field, and before this scenario no list param had ever round-tripped in either
// direction -- optionalStringList sat at zero coverage. Both live-only defects
// found in this feature so far were in exactly this area (params being non-null,
// and the creatable-type rule), and both passed a green mock suite, so the
// untested conversions are where the next one would be.
func PageModuleListParamsSteps(prefix string) []resource.TestStep {
	collection := providerBlock + fmt.Sprintf(`
resource "datahub_page_module" "test" {
  page_module_id = "%[1]s-coll"
  name           = "TF Test Collection"
  type           = "ASSET_COLLECTION"

  params = {
    asset_collection = {
      asset_urns = ["urn:li:domain:tf-test-a", "urn:li:domain:tf-test-b"]
    }
  }
}
`, prefix)

	hierarchy := providerBlock + fmt.Sprintf(`
resource "datahub_page_module" "test2" {
  page_module_id = "%[1]s-hier"
  name           = "TF Test Hierarchy"
  type           = "HIERARCHY"

  params = {
    hierarchy_view = {
      asset_urns            = ["urn:li:domain:tf-test-a"]
      show_related_entities = true
    }
  }
}
`, prefix)

	return []resource.TestStep{
		{
			Config: collection,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("datahub_page_module.test", "params.asset_collection.asset_urns.#", "2"),
				resource.TestCheckResourceAttr("datahub_page_module.test", "params.asset_collection.asset_urns.1",
					"urn:li:domain:tf-test-b"),
				// Order within the list must survive, not merely membership.
				resource.TestCheckResourceAttr("datahub_page_module.test", "params.asset_collection.asset_urns.0",
					"urn:li:domain:tf-test-a"),
			),
		},
		{
			Config: hierarchy,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("datahub_page_module.test2", "params.hierarchy_view.asset_urns.#", "1"),
				resource.TestCheckResourceAttr("datahub_page_module.test2", "params.hierarchy_view.show_related_entities", "true"),
			),
		},
	}
}

// PageModuleImportSteps imports a module created outside Terraform.
//
// datahub_page_module's ImportState had zero coverage while every other
// resource in the provider sat between 53% and 72% -- a shipped, registered
// import target that nothing exercised.
func PageModuleImportSteps(prefix string) []resource.TestStep {
	moduleID := prefix + "-import"

	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_page_module" "imported" {
  page_module_id = %q
  name           = "TF Test Imported"
  type           = "RICH_TEXT"

  params = {
    rich_text = {
      content = "imported"
    }
  }
}
`, moduleID)

	return []resource.TestStep{
		{
			PreConfig:          func() { seedPageModule(moduleID, "TF Test Imported", "imported") },
			Config:             cfg,
			ResourceName:       "datahub_page_module.imported",
			ImportState:        true,
			ImportStateId:      moduleID,
			ImportStatePersist: true,
			ImportStateCheck: func(states []*terraform.InstanceState) error {
				if len(states) != 1 {
					return fmt.Errorf("expected 1 imported state, got %d", len(states))
				}
				attrs := states[0].Attributes
				if got := attrs["type"]; got != "RICH_TEXT" {
					return fmt.Errorf("imported type = %q, want RICH_TEXT", got)
				}
				if got := attrs["params.rich_text.content"]; got != "imported" {
					return fmt.Errorf("imported params did not round-trip: %q", got)
				}
				return nil
			},
		},
	}
}

// PageTemplateRestoreFromStateSteps deletes the DataHub-side backup and checks
// the destroy still restores, from the copy in Terraform state.
//
// This is the fallback rung of the restore ladder, and it is the one that runs
// when something has already gone wrong -- exactly when a bug costs most. The
// CheckDestroy is where the assertion lives, because the restore happens during
// teardown rather than in a step.
func PageTemplateRestoreFromStateSteps(prefix string) (steps []resource.TestStep, checkDestroy resource.TestCheckFunc) {
	templateID := prefix + "-fallback"
	original := [][]string{
		{"urn:li:dataHubPageModule:your_assets"},
		{"urn:li:dataHubPageModule:top_domains"},
	}

	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_page_template" "fallback" {
  page_template_id = %q

  rows = [
    { modules = ["urn:li:dataHubPageModule:platforms"] },
  ]
}
`, templateID)

	steps = []resource.TestStep{
		{
			PreConfig: func() { seedPageTemplate(templateID, original) },
			Config:    cfg,
			Check: resource.TestCheckResourceAttr("datahub_page_template.fallback",
				"original_rows.#", "2"),
		},
		{
			// Remove the authoritative copy, leaving only original_rows in state.
			PreConfig: func() {
				if err := DeleteSeededPageTemplate("tfprovider-backup-" + templateID); err != nil {
					panic(fmt.Sprintf("could not delete the backup template: %v", err))
				}
			},
			Config: cfg,
		},
	}

	checkDestroy = func(*terraform.State) error {
		rows, err := readPageTemplateRows(templateID)
		if err != nil {
			return fmt.Errorf("reading the template after destroy: %w", err)
		}
		if len(rows) != 2 || len(rows[0]) != 1 || rows[0][0] != "urn:li:dataHubPageModule:your_assets" {
			return fmt.Errorf("destroy did not restore the original layout from state, got %v", rows)
		}
		return nil
	}
	return steps, checkDestroy
}

// seedPageModule creates a page module outside Terraform.
func seedPageModule(moduleID, name, content string) {
	body := fmt.Sprintf(`{"query":"mutation u($input: UpsertPageModuleInput!){ upsertPageModule(input:$input){urn} }",`+
		`"variables":{"input":{"urn":"urn:li:dataHubPageModule:%s","name":%q,"type":"RICH_TEXT","scope":"GLOBAL",`+
		`"params":{"richTextParams":{"content":%q}}}}}`, moduleID, name, content)
	postGraphQL(body, "seedPageModule")
}

// readPageTemplateRows reads a template's rows through the strongly-consistent
// entity endpoint, for assertions that run outside a Terraform step.
func readPageTemplateRows(templateID string) ([][]string, error) {
	url := fmt.Sprintf("%s/openapi/v3/entity/datahubpagetemplate/urn:li:dataHubPageTemplate:%s",
		os.Getenv("DATAHUB_GMS_URL"), templateID)
	req, err := http.NewRequest(http.MethodGet, url, nil) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("building the read request: %w", err)
	}
	if tok := os.Getenv("DATAHUB_GMS_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reading the template: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d reading the template", resp.StatusCode)
	}

	var entity struct {
		Props struct {
			Value struct {
				Rows []struct {
					Modules []string `json:"modules"`
				} `json:"rows"`
			} `json:"value"`
		} `json:"dataHubPageTemplateProperties"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("decoding the template: %w", err)
	}
	out := make([][]string, 0, len(entity.Props.Value.Rows))
	for _, r := range entity.Props.Value.Rows {
		out = append(out, r.Modules)
	}
	return out, nil
}

// postGraphQL sends a mutation and panics on transport or HTTP failure, which is
// what a test seeding helper should do: a seed that silently failed would turn
// into a confusing assertion failure several steps later.
func postGraphQL(body, caller string) {
	req, err := http.NewRequest(http.MethodPost, os.Getenv("DATAHUB_GMS_URL")+"/api/graphql", strings.NewReader(body)) //nolint:noctx
	if err != nil {
		panic(fmt.Sprintf("%s: build request: %v", caller, err))
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := os.Getenv("DATAHUB_GMS_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(fmt.Sprintf("%s: POST: %v", caller, err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		panic(fmt.Sprintf("%s: unexpected status %d", caller, resp.StatusCode))
	}
}
