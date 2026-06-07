output "data_quality_lead_urn" {
  description = "URN of the Data Quality Lead ownership type."
  value       = datahub_ownership_type.data_quality_lead.urn
}

output "data_producer_urn" {
  description = "URN of the Data Producer ownership type."
  value       = datahub_ownership_type.data_producer.urn
}

# Keyed by URN; includes built-in system types and any types that existed in
# DataHub before this apply. Newly created types appear here on the next plan
# or refresh once the listOwnershipTypes GraphQL index has caught up.
output "ownership_types" {
  description = "All DataHub ownership types keyed by URN, with type_id, name, and description for each."
  value = {
    for urn, ot in data.datahub_ownership_type.details : urn => {
      type_id     = ot.type_id
      name        = ot.name
      description = ot.description
    }
  }
}
