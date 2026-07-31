# List all data products URNs, e.g. to bulk-import them into Terraform state.
data "datahub_data_products" "all" {}

output "data_product_urns" {
  value = data.datahub_data_products.all.urns
}

# Feed the urns into an import {} for-each block to adopt existing data products
# (requires Terraform >= 1.11 for import for_each):
#
# import {
#   for_each = toset(data.datahub_data_products.all.urns)
#   to       = datahub_data_product.imported[each.value]
#   id       = each.value
# }
