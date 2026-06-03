data "datahub_domain" "finance" {
  domain_id = "finance"
}

output "finance_urn" {
  description = "URN of the Finance domain."
  value       = data.datahub_domain.finance.urn
}
