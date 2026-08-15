# List all metadata test URNs, e.g. to bulk-import them into Terraform state.
# Tests created in the DataHub Cloud UI have random UUID ids, so enumeration
# plus import is how a brownfield deployment comes under Terraform management.
data "datahub_metadata_tests" "all" {}

output "metadata_test_urns" {
  value = data.datahub_metadata_tests.all.urns
}

# Feed the urns into an import {} for-each block to adopt existing tests
# (requires Terraform >= 1.11 for import for_each):
#
# import {
#   for_each = toset(data.datahub_metadata_tests.all.urns)
#   to       = datahub_metadata_test.imported[each.value]
#   id       = each.value
# }
