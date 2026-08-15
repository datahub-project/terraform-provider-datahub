data "datahub_forms" "all" {}

output "form_urns" {
  description = "URNs of all DataHub forms."
  value       = data.datahub_forms.all.urns
}

# Bulk-import every existing form into Terraform state (Terraform >= 1.11):
#
# import {
#   for_each = toset(data.datahub_forms.all.urns)
#   to       = datahub_form.imported[each.key]
#   id       = each.key
# }
