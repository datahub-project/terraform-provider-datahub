# Look up a structured property definition by its id. Useful when a
# datahub_structured_property_assignment must reference a property this
# configuration does not define.
data "datahub_structured_property" "retention_days" {
  property_id = "tf-example-retention-days"
}

output "retention_days_urn" {
  description = "URN of the Retention Days structured property."
  value       = data.datahub_structured_property.retention_days.urn
}
