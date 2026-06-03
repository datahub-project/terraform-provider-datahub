data "datahub_domains" "all" {}

output "domain_urns" {
  description = "URNs of all DataHub domains."
  value       = data.datahub_domains.all.urns
}
