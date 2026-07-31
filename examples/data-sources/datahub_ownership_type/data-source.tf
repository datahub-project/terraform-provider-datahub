# Look up an ownership type by its id, to reference its URN where an owner is
# assigned.
data "datahub_ownership_type" "data_quality_lead" {
  type_id = "tf-example-data-quality-lead"
}

output "data_quality_lead_urn" {
  description = "URN of the Data Quality Lead ownership type."
  value       = data.datahub_ownership_type.data_quality_lead.urn
}
