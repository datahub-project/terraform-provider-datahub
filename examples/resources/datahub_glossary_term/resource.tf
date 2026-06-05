# A term group to hold the terms.
resource "datahub_glossary_node" "finance" {
  node_id     = "finance"
  name        = "Finance"
  description = "Financial metrics and KPIs"
}

# A glossary term beneath the Finance term group.
resource "datahub_glossary_term" "revenue" {
  term_id     = "revenue"
  name        = "Revenue"
  description = "Total revenue recognised in the reporting period"
  parent_node = datahub_glossary_node.finance.urn
}
