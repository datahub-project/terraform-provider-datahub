# List all tags URNs, e.g. to bulk-import them into Terraform state.
data "datahub_tags" "all" {}

output "tag_urns" {
  value = data.datahub_tags.all.urns
}

# Feed the urns into an import {} for-each block to adopt existing tags
# (requires Terraform >= 1.11 for import for_each):
#
# import {
#   for_each = toset(data.datahub_tags.all.urns)
#   to       = datahub_tag.imported[each.value]
#   id       = each.value
# }
