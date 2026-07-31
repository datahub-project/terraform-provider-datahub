# List all assertions URNs, e.g. to bulk-import them into Terraform state.
data "datahub_assertions" "all" {}

output "assertion_urns" {
  value = data.datahub_assertions.all.urns
}

# Feed the urns into an import {} for-each block to adopt existing assertions
# (requires Terraform >= 1.11 for import for_each):
#
# import {
#   for_each = toset(data.datahub_assertions.all.urns)
#   to       = datahub_custom_assertion.imported[each.value]
#   id       = each.value
# }
