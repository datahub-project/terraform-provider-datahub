# A root-level term group (no parent).
resource "datahub_glossary_node" "business" {
  node_id     = "business"
  name        = "Business"
  description = "Top-level business concepts"
}

# A child term group nested under Business.
resource "datahub_glossary_node" "finance" {
  node_id     = "finance"
  name        = "Finance"
  description = "Financial metrics and KPIs"
  parent_node = datahub_glossary_node.business.urn
}
