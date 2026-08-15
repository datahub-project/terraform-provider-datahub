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

# Every form on the instance, for the bulk-import pattern in the README.
#
# Expect the two forms this example just created to be MISSING from this list
# on the apply that creates them, and expect forms you did not create to be
# present. The plural data source is backed by searchAcrossEntities, which is
# index-backed and eventually consistent, so a just-written form has not been
# indexed yet. It appears on the next plan or refresh. This is not a failed
# apply, and the strongly consistent read below is the one to trust.
output "all_form_urns" {
  description = "All form URNs on the instance. Index-backed: the forms just created are usually absent until the next refresh."
  value       = data.datahub_forms.all.urns
}

# Reads one created form back through the OpenAPI v3 entity endpoint, which is
# MySQL-backed and strongly consistent -- so unlike the list above it shows the
# form immediately after apply. Copy it from the output, or:
#   eval "$(terraform output -raw verify_command)"
output "verify_command" {
  description = "Shell command that fetches the COMPLETION form via the strongly consistent OpenAPI v3 endpoint."
  value       = "curl -sS -H \"Authorization: Bearer $DATAHUB_GMS_TOKEN\" \"$DATAHUB_GMS_URL/openapi/v3/entity/form/${datahub_form.dataset_metadata.urn}\""
}

output "summary" {
  description = "Post-apply summary: what was created, where to verify it, and what will not have happened yet."
  value       = <<-EOT

  Forms created (2), plus the property and group they depend on:

    TF Example Form - Dataset Metadata      ${datahub_form.dataset_metadata.urn}
      COMPLETION, assigned to asset owners + the stewards group
    TF Example Form - Sensitivity           ${datahub_form.sensitivity_attestation.urn}
      VERIFICATION, assigned to asset owners

    Structured property collected           ${datahub_structured_property.sensitivity.urn}
    Stewardship group                       ${datahub_corp_group.stewards.urn}

  Verify in the DataHub UI:
    Govern -> Forms. Both forms appear immediately.

  Two things that will NOT be true yet, and neither is a failure:

    1. all_form_urns above probably omits both new forms. That list is
       index-backed and lags the write; the entity read does not:

         eval "$(terraform output -raw verify_command)"

    2. No asset carries either form yet. Assignment runs in a background
       hook against datasets matching the filter, so nothing is assigned
       until a matching dataset exists and the hook has run. A fresh
       Quickstart has no PostgreSQL datasets at all, so nothing will ever
       match there -- the forms are correct, the estate is empty.

  To remove all resources:
    terraform destroy

  EOT
}
