data "datahub_glossary_node" "finance" {
  node_id = "finance"
}

output "finance_urn" {
  value = data.datahub_glossary_node.finance.urn
}
