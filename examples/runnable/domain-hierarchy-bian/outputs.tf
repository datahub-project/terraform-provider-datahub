output "business_area_count" {
  description = "Number of BIAN business-area domains created."
  value       = length(datahub_domain.business_area)
}

output "business_domain_count" {
  description = "Number of BIAN business-domain nodes created."
  value       = length(datahub_domain.business_domain)
}

output "service_domain_count" {
  description = "Number of BIAN service-domain leaf nodes created."
  value       = length(datahub_domain.service_domain)
}

output "total_domains" {
  description = "Total number of DataHub domains created across all three BIAN hierarchy levels."
  value       = length(datahub_domain.business_area) + length(datahub_domain.business_domain) + length(datahub_domain.service_domain)
}

output "business_area_urns" {
  description = "Map of BIAN business-area id to DataHub domain URN for each top-level area created."
  value       = { for k, d in datahub_domain.business_area : k => d.urn }
}
