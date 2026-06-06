output "retention_days_urn" {
  description = "DataHub URN for the retention-days structured property."
  value       = datahub_structured_property.retention_days.urn
}

output "classification_urn" {
  description = "DataHub URN for the classification structured property."
  value       = datahub_structured_property.classification.urn
}

output "retention_lookup_display_name" {
  description = "Display name of the retention-days property as read back via the data source."
  value       = data.datahub_structured_property.retention_lookup.display_name
}

output "all_structured_property_urns" {
  description = "All structured property URNs in DataHub (eventually consistent)."
  value       = data.datahub_structured_properties.all.urns
}

output "verify_url" {
  description = "URL to verify the structured properties in the DataHub UI (replace with your DATAHUB_GMS_URL)."
  value       = "http://localhost:8080/structured-properties"
}
