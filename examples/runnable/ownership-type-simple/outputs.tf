output "data_quality_lead_urn" {
  description = "URN of the Data Quality Lead ownership type."
  value       = datahub_ownership_type.data_quality_lead.urn
}

output "data_producer_urn" {
  description = "URN of the Data Producer ownership type."
  value       = datahub_ownership_type.data_producer.urn
}

output "owned_term_urn" {
  description = "URN of the glossary term the owners are assigned to. This is also the import id for the datahub_entity_ownership resource."
  value       = datahub_glossary_term.customer_ltv.urn
}

output "owner_group_urn" {
  description = "URN of the group named as an owner."
  value       = datahub_corp_group.analytics_stewards.urn
}

# The (owner, ownership type) pairs this configuration manages, rendered as
# "owner -> role" so the two demonstrated shapes are visible at a glance: two
# owners in the same role, and one owner holding two roles.
output "owner_assignments" {
  description = "Every (owner, ownership type) pair managed on the glossary term."
  value = [
    for o in datahub_entity_ownership.customer_ltv.owner
    : "${o.owner_urn} -> ${o.ownership_type_urn}"
  ]
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

# Read the owners back straight from the strongly-consistent entity endpoint,
# which is where the provider itself reads them. Run it with
# eval "$(terraform output -raw verify_owners_command)" -- the environment
# variables are expanded by the shell at that point, not by Terraform.
output "verify_owners_command" {
  description = "curl command that prints the glossary term's ownership aspect from the OpenAPI v3 entity endpoint."
  value = join(" ", [
    "curl -sS -H \"Authorization: Bearer $DATAHUB_GMS_TOKEN\"",
    "\"$DATAHUB_GMS_URL/openapi/v3/entity/glossaryterm/${datahub_glossary_term.customer_ltv.urn}\"",
    "| jq .ownership.value.owners",
  ])
}

# A path rather than a full URL: an output value is never passed through a
# shell, so a literal $DATAHUB_GMS_URL inside one would be printed verbatim and
# expand nowhere. Append this to your DataHub base URL.
output "term_ui_path" {
  description = "Path of the owned glossary term's page in the DataHub UI; append it to your DataHub base URL and read the Ownership panel."
  value       = "/glossaryTerm/${datahub_glossary_term.customer_ltv.urn}"
}
