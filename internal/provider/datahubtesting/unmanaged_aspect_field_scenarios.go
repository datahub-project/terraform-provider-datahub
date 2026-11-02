// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"fmt"
	"os"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// UnmanagedFieldCase is one resource whose aspect write must keep the fields
// it does not manage.
type UnmanagedFieldCase struct {
	// Name labels the subtest.
	Name string
	// URN is the entity the resource manages.
	URN string
	// Aspect is the aspect the resource writes whole.
	Aspect string
	// Create is the first configuration; Update changes a managed attribute
	// that makes the resource write Aspect again.
	Create, Update string
}

// unmanagedSentinelField is a field no DataHub aspect defines, standing in for
// every field the provider does not model.
const unmanagedSentinelField = "tfUnmanagedFieldSentinel"

// UnmanagedFieldCases returns one case per resource that preserves the fields
// it does not manage on an OpenAPI v3 aspect write. base must already be
// target-scoped (tg.Name). It mirrors the preserved aspects classified in
// pkg/datahub's aspect write guard, at the resource level: the guard proves
// each client writer carries unmanaged fields over, these prove each resource's
// update path goes through that writer.
func UnmanagedFieldCases(base string) []UnmanagedFieldCase {
	domain := func(desc string) string {
		return providerBlock + fmt.Sprintf(`
resource "datahub_domain" "test" {
  domain_id         = %q
  name              = "Unmanaged Field Domain"
  custom_properties = { revision = %q }
}
`, base+"-domain", desc)
	}
	dataProduct := func(desc string) string {
		return providerBlock + fmt.Sprintf(`
resource "datahub_domain" "home" {
  domain_id = %q
  name      = "Unmanaged Field Product Home"
}

resource "datahub_data_product" "test" {
  data_product_id = %q
  name            = "Unmanaged Field Product"
  description     = %q
  domain          = datahub_domain.home.urn
}
`, base+"-dp-home", base+"-dp", desc)
	}
	glossaryNode := func(rev string) string {
		return providerBlock + fmt.Sprintf(`
resource "datahub_glossary_node" "test" {
  node_id           = %q
  name              = "Unmanaged Field Node"
  custom_properties = { revision = %q }
}
`, base+"-node", rev)
	}
	glossaryTerm := func(rev string) string {
		return providerBlock + fmt.Sprintf(`
resource "datahub_glossary_term" "test" {
  term_id           = %q
  name              = "Unmanaged Field Term"
  custom_properties = { revision = %q }
}
`, base+"-term", rev)
	}
	corpUser := func(title string) string {
		return providerBlock + fmt.Sprintf(`
resource "datahub_corp_user" "test" {
  username     = %q
  display_name = "Unmanaged Field User"
  title        = %q
}
`, base+"-user", title)
	}
	serviceAccount := func(desc string) string {
		return providerBlock + fmt.Sprintf(`
resource "datahub_service_account" "test" {
  service_account_id = %q
  display_name       = "Unmanaged Field Service Account"
  description        = %q
}
`, base+"-sa", desc)
	}
	structuredProperty := func(desc string, hidden bool) string {
		return providerBlock + fmt.Sprintf(`
resource "datahub_structured_property" "test" {
  property_id  = %q
  value_type   = "string"
  entity_types = ["dataset"]
  description  = %q
  settings = {
    is_hidden = %t
  }
}
`, base+".sp", desc, hidden)
	}
	ownershipType := func(desc string) string {
		return providerBlock + fmt.Sprintf(`
resource "datahub_ownership_type" "test" {
  type_id     = %q
  name        = "Unmanaged Field Ownership Type"
  description = %q
}
`, base+"-otype", desc)
	}
	// The recipe, not source_name, is what changes: source_name forces
	// replacement, which rightly starts from a new entity.
	ingestionSource := func(rev string) string {
		return providerBlock + fmt.Sprintf(`
resource "datahub_ingestion_source" "test" {
  source_id   = %q
  source_name = "Unmanaged Field Source"
  recipe      = jsonencode({ source = { type = "demo-data", config = { revision = %q } } })
}
`, base+"-ingest", rev)
	}

	return []UnmanagedFieldCase{
		{"domain", "urn:li:domain:" + base + "-domain", "domainProperties", domain("one"), domain("two")},
		{"data_product", "urn:li:dataProduct:" + base + "-dp", "dataProductProperties", dataProduct("one"), dataProduct("two")},
		{"glossary_node", "urn:li:glossaryNode:" + base + "-node", "glossaryNodeInfo", glossaryNode("one"), glossaryNode("two")},
		{"glossary_term", "urn:li:glossaryTerm:" + base + "-term", "glossaryTermInfo", glossaryTerm("one"), glossaryTerm("two")},
		{"corp_user", "urn:li:corpuser:" + base + "-user", "corpUserInfo", corpUser("one"), corpUser("two")},
		{"service_account", "urn:li:corpuser:service_" + base + "-sa", "corpUserInfo", serviceAccount("one"), serviceAccount("two")},
		{"structured_property_definition", "urn:li:structuredProperty:" + base + ".sp", "propertyDefinition", structuredProperty("one", false), structuredProperty("two", false)},
		{"structured_property_settings", "urn:li:structuredProperty:" + base + ".sp", "structuredPropertySettings", structuredProperty("one", false), structuredProperty("one", true)},
		{"ownership_type", "urn:li:ownershipType:" + base + "-otype", "ownershipTypeInfo", ownershipType("one"), ownershipType("two")},
		{"ingestion_source", "urn:li:dataHubIngestionSource:" + base + "-ingest", "dataHubIngestionSourceInfo", ingestionSource("one"), ingestionSource("two")},
	}
}

// UnmanagedFieldSurvivesUpdateSteps creates the resource, seeds a field it
// does not manage on its aspect, then applies a change that makes it write
// the aspect again, and asserts the field survived. The second step's
// automatic empty-plan check also proves the seeded field causes no drift.
// Mock-only: it seeds through /test-control/seed-unmanaged-field.
func UnmanagedFieldSurvivesUpdateSteps(c UnmanagedFieldCase) []resource.TestStep {
	return []resource.TestStep{
		{Config: c.Create},
		{
			PreConfig: func() {
				SeedUnmanagedAspectField(os.Getenv("DATAHUB_GMS_URL"), c.URN, c.Aspect, unmanagedSentinelField, "keep-me")
			},
			Config: c.Update,
			Check:  CheckUnmanagedAspectField(c.URN, c.Aspect, unmanagedSentinelField, "keep-me"),
		},
	}
}
