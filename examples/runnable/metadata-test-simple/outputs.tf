output "description_test_urn" {
  description = "URN of the dataset-description metadata test."
  value       = datahub_metadata_test.datasets_have_descriptions.urn
}

output "prod_owner_test_urn" {
  description = "URN of the PROD-ownership metadata test."
  value       = datahub_metadata_test.prod_datasets_have_owners.urn
}

# All metadata test URNs on the instance, from the plural data source. This
# list is the input to the bulk-import pattern in the README: feed it into an
# import {} for-each block to adopt tests created in the DataHub UI (which
# get random UUID ids) without hand-authoring resource blocks.
#
# listTests is eventually consistent, so the two tests created by this
# example may be absent from this list on the apply that creates them and
# appear on the next plan or refresh.
output "all_metadata_test_urns" {
  description = "URNs of every metadata test visible to the authenticated principal."
  value       = data.datahub_metadata_tests.all.urns
}

# Complete command to read one created test back through the strongly
# consistent OpenAPI v3 entity endpoint. Copy it from the apply output, or:
#   eval "$(terraform output -raw verify_command)"
# The DATAHUB_* variables expand in your shell at run time.
output "verify_command" {
  description = "Shell command that fetches the description test entity via the OpenAPI v3 endpoint."
  value       = "curl -sS -H \"Authorization: Bearer $DATAHUB_GMS_TOKEN\" \"$DATAHUB_GMS_URL/openapi/v3/entity/test/${datahub_metadata_test.datasets_have_descriptions.urn}\""
}
