# A root domain (no parent).
resource "datahub_domain" "finance" {
  domain_id   = "finance"
  name        = "Finance"
  description = "Finance and treasury data assets"
}

# A child domain nested under the root.
# Referencing .urn (not a raw string) gives Terraform the dependency edge so
# the parent is created first and destroyed last -- required because DataHub
# refuses to delete a domain that has child domains.
resource "datahub_domain" "credit_risk" {
  domain_id     = "credit-risk"
  name          = "Credit Risk"
  description   = "Credit risk models and exposure data"
  parent_domain = datahub_domain.finance.urn
}

output "finance_urn" {
  description = "URN of the Finance root domain."
  value       = datahub_domain.finance.urn
}

output "credit_risk_urn" {
  description = "URN of the Credit Risk child domain."
  value       = datahub_domain.credit_risk.urn
}
