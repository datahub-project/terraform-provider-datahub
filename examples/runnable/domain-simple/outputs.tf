output "finance_domain_urn" {
  description = "URN of the Finance root domain."
  value       = datahub_domain.finance.urn
}

output "accounting_domain_urn" {
  description = "URN of the Accounting domain (child of Finance)."
  value       = datahub_domain.accounting.urn
}

output "treasury_domain_urn" {
  description = "URN of the Treasury domain (child of Finance)."
  value       = datahub_domain.treasury.urn
}

output "engineering_domain_urn" {
  description = "URN of the Engineering root domain."
  value       = datahub_domain.engineering.urn
}

output "data_platform_domain_urn" {
  description = "URN of the Data Platform domain (child of Engineering)."
  value       = datahub_domain.data_platform.urn
}

output "analytics_domain_urn" {
  description = "URN of the Analytics domain (child of Engineering)."
  value       = datahub_domain.analytics.urn
}

output "summary" {
  description = "Post-apply summary of all created domains."
  value       = <<-EOT

  Domain hierarchy created (2 root domains, 4 child domains):

    TF Example - Finance              ${datahub_domain.finance.urn}
      +- TF Example - Accounting      ${datahub_domain.accounting.urn}
      +- TF Example - Treasury        ${datahub_domain.treasury.urn}

    TF Example - Engineering          ${datahub_domain.engineering.urn}
      +- TF Example - Data Platform   ${datahub_domain.data_platform.urn}
      +- TF Example - Analytics       ${datahub_domain.analytics.urn}

  View in DataHub UI:
    $DATAHUB_GMS_URL/domains

  To remove all resources:
    terraform destroy

  EOT
}
