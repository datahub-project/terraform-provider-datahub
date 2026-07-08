terraform {
  required_version = ">= 1.11"
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.15.0"
    }
  }
}

provider "datahub" {
  # Credentials from environment:
  #   DATAHUB_GMS_URL   - e.g. https://your-instance.acryl.io/gms  (or http://localhost:8080 for OSS)
  #   DATAHUB_GMS_TOKEN - personal access token
}

# ---------------------------------------------------------------------------
# This example contrasts the two kinds of properties DataHub supports, on
# glossary entities (which render both in the UI):
#
#   * custom_properties  - a free-form key/value map owned per entity. Flat,
#                          unvalidated, defined inline on the entity.
#   * structured properties - defined once (with a value type + allowed values),
#                          assigned to entities of one or more types, validated
#                          server-side, and folded into a group in the UI by
#                          their dotted qualified name.
#
# It builds a tiny glossary tree (one node + one term) and one structured
# property reused across BOTH entity types.
# ---------------------------------------------------------------------------

# ---------------------------------------------------------------------------
# Glossary node + term. The term also carries custom_properties -- the flat,
# per-entity kind. custom_properties is Terraform-owned: keys added outside
# Terraform are removed on the next apply.
# ---------------------------------------------------------------------------

resource "datahub_glossary_node" "governance" {
  node_id     = "tf-example-governance"
  name        = "TF Example - Governance"
  description = "Governance concepts used to demonstrate structured and custom properties"
}

resource "datahub_glossary_term" "revenue" {
  term_id     = "tf-example-revenue"
  name        = "TF Example Revenue"
  description = "Total revenue recognised in the reporting period"
  parent_node = datahub_glossary_node.governance.urn

  # Custom properties: flat key/value metadata on this term. Renders in the
  # term's Properties tab OUTSIDE the structured-property group.
  custom_properties = {
    steward       = "data-office"
    source_system = "SITS"
  }
}

# ---------------------------------------------------------------------------
# Structured property definitions.
#
# Both ids share the "tf-example.governance." prefix. DataHub's entity
# Properties tab folds structured properties into a group by the dotted
# qualified name, so on the term these two collapse under a single
# "tf-example.governance" wrapper -- the grouping is derived from the name,
# it is not a first-class object you manage separately.
# ---------------------------------------------------------------------------

# Multi-valued, reused across two entity types (glossaryTerm AND glossaryNode).
# entity_types must be the exact camelCase short names, or an assignment to
# that entity type is rejected as "not applicable".
resource "datahub_structured_property" "regions" {
  property_id  = "tf-example.governance.regions"
  display_name = "TF Example - Regions"
  description  = "Regions this asset applies to. Multi-valued; drawn from a fixed set."
  value_type   = "string"
  cardinality  = "MULTIPLE"
  entity_types = ["glossaryTerm", "glossaryNode"]

  allowed_values = [
    { string_value = "GLOBAL", description = "Applies worldwide" },
    { string_value = "APAC", description = "Asia-Pacific" },
    { string_value = "EMEA", description = "Europe, Middle East, Africa" },
    { string_value = "AMER", description = "Americas" },
  ]

  settings = {
    show_in_search_filters = true
  }
}

# Single-valued, scoped to glossary terms only. Exists to give the folding
# group a second member on the term (and to show a SINGLE controlled property).
resource "datahub_structured_property" "tier" {
  property_id  = "tf-example.governance.tier"
  display_name = "TF Example - Tier"
  description  = "Curation tier of the term. Single-valued."
  value_type   = "string"
  cardinality  = "SINGLE"
  entity_types = ["glossaryTerm"]

  allowed_values = [
    { string_value = "Gold" },
    { string_value = "Silver" },
    { string_value = "Bronze" },
  ]
}

# ---------------------------------------------------------------------------
# Assignments. Each resource is one (entity, property) edge with a value list.
# The "regions" property is assigned to BOTH the node and the term, sharing
# the value GLOBAL, with one region unique to each. AMER is an allowed value
# that is deliberately left unassigned.
# ---------------------------------------------------------------------------

resource "datahub_structured_property_assignment" "regions_node" {
  entity_urn              = datahub_glossary_node.governance.urn
  structured_property_urn = datahub_structured_property.regions.urn
  values                  = ["GLOBAL", "EMEA"]
}

resource "datahub_structured_property_assignment" "regions_term" {
  entity_urn              = datahub_glossary_term.revenue.urn
  structured_property_urn = datahub_structured_property.regions.urn
  values                  = ["GLOBAL", "APAC"]
}

# Second assignment on the SAME entity (the term). depends_on serialises it
# after regions_term so the two writes are not sent concurrently: on this
# provider version, concurrent structured-property writes to one entity race
# server-side and can lose a value. (A per-entity serialization fix in the
# provider makes this belt-and-suspenders on newer versions.)
resource "datahub_structured_property_assignment" "tier_term" {
  entity_urn              = datahub_glossary_term.revenue.urn
  structured_property_urn = datahub_structured_property.tier.urn
  values                  = ["Gold"]

  depends_on = [datahub_structured_property_assignment.regions_term]
}

# DataHub Provider and/or DataHub (but see CAT-2563) should block this assignment
resource "datahub_structured_property_assignment" "tier_term_for_glossary_node" {
  entity_urn              = datahub_glossary_node.governance.urn
  structured_property_urn = datahub_structured_property.tier.urn
  values                  = ["Gold"]

  depends_on = [datahub_structured_property_assignment.tier_term]
}
