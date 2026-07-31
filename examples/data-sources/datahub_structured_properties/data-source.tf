# List all structured properties URNs, e.g. to bulk-import them into Terraform state.
data "datahub_structured_properties" "all" {}

output "structured_property_urns" {
  value = data.datahub_structured_properties.all.urns
}

# Feed the urns into an import {} for-each block to adopt existing structured properties
# (requires Terraform >= 1.11 for import for_each):
#
# import {
#   for_each = toset(data.datahub_structured_properties.all.urns)
#   to       = datahub_structured_property.imported[each.value]
#   id       = each.value
# }
