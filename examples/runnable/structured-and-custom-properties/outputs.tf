output "node_urn" {
  description = "URN of the governance term group."
  value       = datahub_glossary_node.governance.urn
}

output "term_urn" {
  description = "URN of the Revenue term (carries custom_properties + structured properties)."
  value       = datahub_glossary_term.revenue.urn
}

output "regions_property_urn" {
  description = "URN of the multi-valued Regions structured property (assigned to node + term)."
  value       = datahub_structured_property.regions.urn
}

output "tier_property_urn" {
  description = "URN of the single-valued Tier structured property (term-scoped)."
  value       = datahub_structured_property.tier.urn
}

output "assignment_ids" {
  description = "Composite ids (<entity_urn>|<structured_property_urn>) of the three assignments."
  value = [
    datahub_structured_property_assignment.regions_node.id,
    datahub_structured_property_assignment.regions_term.id,
    datahub_structured_property_assignment.tier_term.id,
  ]
}

output "node_custom_properties_all" {
  description = "The node's merged custom_properties_all: defaults.custom_properties' \"team\" key + the auto_properties managed-by marker (the node sets no custom_properties of its own)."
  value       = datahub_glossary_node.governance.custom_properties_all
}

output "term_custom_properties_all" {
  description = "The term's merged custom_properties_all: its own steward/source_system, plus defaults.custom_properties' \"team\" key and the managed-by marker -- all three sources coexist because none of their keys collide."
  value       = datahub_glossary_term.revenue.custom_properties_all
}

output "summary" {
  description = "Post-apply summary of the properties created and assigned."
  value       = <<-EOT

  Glossary tree + properties created:

    TF Example Governance - Concepts      ${datahub_glossary_node.governance.urn}
      custom:     team = data-platform, managed-by = terraform          (from provider defaults)
      structured: Regions = [GLOBAL, EMEA]
      +- TF Example Governance - Revenue  ${datahub_glossary_term.revenue.urn}
           custom:     steward = data-office, source_system = SITS      (its own)
                       team = data-platform, managed-by = terraform     (from provider defaults)
           structured: Regions = [GLOBAL, APAC], Tier = Gold            (folded under tf-example.governance)

  Structured property "Regions" allows GLOBAL/APAC/EMEA/AMER; AMER is defined
  but deliberately left unassigned. GLOBAL is shared by both entities.

  View in DataHub UI:
    $DATAHUB_GMS_URL/glossary   -> open "TF Example Governance - Revenue" -> Properties tab

    On the term, the two structured properties fold under a single
    "tf-example.governance" group (grouping is derived from the dotted
    qualified name), while the custom properties render flat, separately.

  To remove all resources:
    terraform destroy

  EOT
}
