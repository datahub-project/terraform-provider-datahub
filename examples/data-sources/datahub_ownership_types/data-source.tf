# List all ownership types URNs, e.g. to bulk-import them into Terraform state.
data "datahub_ownership_types" "all" {}

output "ownership_type_urns" {
  value = data.datahub_ownership_types.all.urns
}

# Feed the urns into an import {} for-each block to adopt existing ownership types
# (requires Terraform >= 1.11 for import for_each):
#
# import {
#   for_each = toset(data.datahub_ownership_types.all.urns)
#   to       = datahub_ownership_type.imported[each.value]
#   id       = each.value
# }
