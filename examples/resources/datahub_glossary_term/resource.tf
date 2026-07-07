# A term group to hold the terms.
resource "datahub_glossary_node" "finance" {
  node_id     = "finance"
  name        = "Finance"
  description = "Financial metrics and KPIs"
}

# A glossary term beneath the Finance term group, with custom properties.
# Terraform owns the complete map; keys added outside Terraform are removed on
# the next apply.
resource "datahub_glossary_term" "revenue" {
  term_id     = "revenue"
  name        = "Revenue"
  description = "Total revenue recognised in the reporting period"
  parent_node = datahub_glossary_node.finance.urn
  custom_properties = {
    steward = "finance"
    tier    = "gold"
  }
}
