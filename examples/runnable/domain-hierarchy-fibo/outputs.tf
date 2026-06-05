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
