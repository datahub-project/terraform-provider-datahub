output "contract_urn" {
  description = "URN of the data contract created against the ingested dataset."
  value       = datahub_data_contract.sample.urn
}

output "assertion_urn" {
  description = "URN of the custom assertion the contract bundles."
  value       = datahub_custom_assertion.freshness.urn
}

output "dataset_urn" {
  description = "URN of the ingested dataset the contract attaches to. NOT managed by Terraform -- see cleanup outputs."
  value       = local.dataset_urn
}

output "ingestion_source_urn" {
  description = "URN of the ingestion source that produced the dataset."
  value       = local.ingestion_source_urn
}

# The dataset outlives terraform destroy, and that is the honest cost of this
# example. Terraform did not create it -- an ingestion run did -- so Terraform
# will not remove it, and neither will the live-example harness, which only
# tracks resources it manages. Both commands below are complete and ready to
# run; pick whichever tool you have.
#
# Note the demo pack contains nine datasets, not one. This removes only the
# dataset the contract pointed at; see the README to sweep the rest.
output "cleanup_dataset_cli" {
  description = "DataHub CLI command that hard-deletes the ingested dataset."
  value       = "datahub delete --urn '${local.dataset_urn}' --hard"
}

output "cleanup_dataset_curl" {
  description = "curl equivalent, for when the DataHub CLI is not installed. Run with: eval \"$(terraform output -raw cleanup_dataset_curl)\""
  value       = "curl -sS -X DELETE -H \"Authorization: Bearer $DATAHUB_GMS_TOKEN\" \"$DATAHUB_GMS_URL/openapi/v3/entity/dataset/${local.dataset_urn_encoded}\""
}

output "summary" {
  description = "Post-apply summary: what was created, what Terraform owns, and what it does not."
  value       = <<-EOT

  Created by Terraform (removed by terraform destroy):

    Ingestion source   ${local.ingestion_source_urn}
    Custom assertion   ${datahub_custom_assertion.freshness.urn}
    Data contract      ${datahub_data_contract.sample.urn}

  Created by the ingestion run (NOT removed by terraform destroy):

    Dataset            ${local.dataset_urn}
    ...plus the eight other datasets in DataHub's bootstrap demo pack.

  This asymmetry is the point of the example. A contract needs a dataset,
  datasets come from ingestion, and ingestion is a verb Terraform cannot
  express -- so the run is a provisioner and its output is unmanaged.

  Verify in the DataHub UI:
    Open the dataset, then the Quality tab. The contract and its assertion
    appear there.

  Clean up the dataset after destroying:
    eval "$(terraform output -raw cleanup_dataset_cli)"
    or
    eval "$(terraform output -raw cleanup_dataset_curl)"

  EOT
}
