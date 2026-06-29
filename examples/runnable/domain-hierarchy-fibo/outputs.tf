output "root_urn" {
  description = "URN of the top-level FIBO root domain (empty when create_root_node is false)."
  value       = var.create_root_node ? datahub_domain.fibo_root[0].urn : ""
}

output "domain_count" {
  description = "Number of top-level FIBO domain nodes created."
  value       = length(datahub_domain.fibo_domain)
}

output "module_count" {
  description = "Number of FIBO module nodes created."
  value       = length(datahub_domain.fibo_module)
}

output "leaf_count" {
  description = "Number of FIBO leaf ontology nodes created."
  value       = length(datahub_domain.fibo_leaf)
}

output "total_domains" {
  description = "Total DataHub domains created (root + domains + modules + leaves)."
  value       = (var.create_root_node ? 1 : 0) + length(datahub_domain.fibo_domain) + length(datahub_domain.fibo_module) + length(datahub_domain.fibo_leaf)
}

output "domain_urns" {
  description = "Map of FIBO domain code to DataHub domain URN for each top-level domain created."
  value       = { for k, d in datahub_domain.fibo_domain : k => d.urn }
}

output "glossary_node_count" {
  description = "Total glossary nodes created (root + domain + module + leaf levels)."
  value = (
    (var.create_glossary && var.create_root_node ? 1 : 0)
    + length(datahub_glossary_node.fibo_glossary_domain)
    + length(datahub_glossary_node.fibo_glossary_module)
    + length(datahub_glossary_node.fibo_glossary_leaf)
  )
}

output "glossary_term_count" {
  description = "Total glossary terms created (owl:Class definitions extracted from leaf ontologies)."
  value       = length(datahub_glossary_term.fibo_term)
}

output "dq_assertion_counts" {
  description = "Data quality assertion counts by type across the 26 ISO 20022 PostgreSQL datasets."
  value = {
    schema    = length(datahub_schema_assertion.iso20022)
    volume    = length(datahub_volume_assertion.iso20022)
    field     = length(datahub_field_assertion.iso20022)
    sql       = length(datahub_sql_assertion.iso20022)
    freshness = length(datahub_freshness_assertion.iso20022)
    total = (
      length(datahub_schema_assertion.iso20022) +
      length(datahub_volume_assertion.iso20022) +
      length(datahub_field_assertion.iso20022) +
      length(datahub_sql_assertion.iso20022) +
      length(datahub_freshness_assertion.iso20022)
    )
  }
}
