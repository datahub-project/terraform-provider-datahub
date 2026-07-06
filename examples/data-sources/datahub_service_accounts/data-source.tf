# List all service account URNs, e.g. to bulk-import them into Terraform state.
data "datahub_service_accounts" "all" {}

output "service_account_urns" {
  value = data.datahub_service_accounts.all.urns
}

# Feed the urns into an import {} for-each block to adopt existing service
# accounts (requires Terraform >= 1.11 for import for_each):
#
# import {
#   for_each = toset(data.datahub_service_accounts.all.urns)
#   to       = datahub_service_account.imported[each.value]
#   id       = each.value
# }
