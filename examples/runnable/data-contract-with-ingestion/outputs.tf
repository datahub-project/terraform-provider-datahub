output "contract_urn" {
  description = "URN of the data contract created against the ingested dataset."
  value       = datahub_data_contract.sample.urn
}

output "assertion_urn" {
  description = "URN of the custom assertion the contract bundles."
  value       = datahub_custom_assertion.freshness.urn
}

# The contract will read "not yet validated" until something reports a result,
# and nothing ever will on its own. A custom assertion is externally evaluated:
# DataHub stores the definition and never runs it, because the whole point of
# the type is that some other system does the checking and reports back. Only
# the typed assertion resources -- freshness, volume, sql, field, schema, all
# Cloud-only -- are evaluated by DataHub itself.
#
# So this is the missing half of the demonstration. Run it and the contract
# turns green, which is what makes the example show a complete story rather
# than a structure with nothing flowing through it.
output "report_passing_result_command" {
  description = "Reports a SUCCESS result against the assertion, so the contract shows as validated. Run with: eval \"$(terraform output -raw report_passing_result_command)\""
  value = format(
    "curl -sS -X POST \"$DATAHUB_GMS_URL/api/graphql\" -H \"Authorization: Bearer $DATAHUB_GMS_TOKEN\" -H 'Content-Type: application/json' -d '%s'",
    jsonencode({
      query = format(
        "mutation { reportAssertionResult(urn: \\\"%s\\\", result: { type: SUCCESS }) }",
        datahub_custom_assertion.freshness.urn,
      )
    })
  )
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
# tracks resources it manages.
#
# CAPTURE THESE BEFORE YOU DESTROY. Outputs are read from state and destroy
# empties it, so `terraform output` returns nothing afterwards -- which is
# exactly when you want the cleanup command. That holds even here, where both
# values are built from a hardcoded URN and depend on no resource at all;
# emptying the state removes the outputs regardless. The README carries the
# same two commands as literals for that reason, and they work at any time.
#
# Note the demo pack contains nine datasets, not one. These remove only the
# dataset the contract pointed at; see the README to sweep the rest.
output "cleanup_dataset_cli" {
  description = "DataHub CLI command that hard-deletes the ingested dataset. Read it BEFORE terraform destroy; see the README for a copy that survives."
  value       = "datahub delete --urn '${local.dataset_urn}' --hard"
}

output "cleanup_dataset_curl" {
  description = "curl equivalent, for when the DataHub CLI is not installed. Read it BEFORE terraform destroy; see the README for a copy that survives."
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
    appear there, and the contract reads "not yet validated".

    That is expected and permanent. A custom assertion is evaluated by an
    external system, not by DataHub, so nothing will ever report a result
    on its own. Supply one and the contract turns green:

      eval "$(terraform output -raw report_passing_result_command)"

  Clean up the dataset after destroying. Copy the command NOW: destroy
  empties the state, and terraform output reads from state, so it will
  return nothing by the time you need it. The README has the same
  command as a literal if you miss this window.

    ${"datahub delete --urn '${local.dataset_urn}' --hard"}

  EOT
}
