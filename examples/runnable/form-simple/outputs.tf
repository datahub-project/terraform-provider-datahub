output "completion_form_urn" {
  description = "URN of the COMPLETION form (urn:li:form:<form_id>)."
  value       = datahub_form.dataset_metadata.urn
}

output "verification_form_urn" {
  description = "URN of the VERIFICATION form."
  value       = datahub_form.sensitivity_attestation.urn
}

output "sensitivity_property_urn" {
  description = "URN of the structured property both forms collect."
  value       = datahub_structured_property.sensitivity.urn
}

output "stewards_group_urn" {
  description = "URN of the group assigned the COMPLETION form."
  value       = datahub_corp_group.stewards.urn
}

output "all_form_urns" {
  description = "All form URNs in DataHub (eventually consistent)."
  value       = data.datahub_forms.all.urns
}
