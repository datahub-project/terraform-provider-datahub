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

output "summary" {
  description = "Post-apply summary of the properties created and assigned."
  value       = <<-EOT

  Glossary tree + properties created:

    TF Example - Governance   ${datahub_glossary_node.governance.urn}
      structured: Regions = [GLOBAL, EMEA]
      +- TF Example Revenue   ${datahub_glossary_term.revenue.urn}
           custom:     steward = data-office, source_system = SITS   (flat)
           structured: Regions = [GLOBAL, APAC], Tier = Gold         (folded under tf-example.governance)

  Structured property "Regions" allows GLOBAL/APAC/EMEA/AMER; AMER is defined
  but deliberately left unassigned. GLOBAL is shared by both entities.

  View in DataHub UI:
    $DATAHUB_GMS_URL/glossary   -> open "TF Example Revenue" -> Properties tab

    On the term, the two structured properties fold under a single
    "tf-example.governance" group (grouping is derived from the dotted
    qualified name), while the custom properties render flat, separately.

  To remove all resources:
    terraform destroy

  EOT
}
