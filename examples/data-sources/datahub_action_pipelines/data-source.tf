# List all action pipelines URNs, e.g. to bulk-import them into Terraform state.
data "datahub_action_pipelines" "all" {}

output "action_pipeline_urns" {
  value = data.datahub_action_pipelines.all.urns
}

# Feed the urns into an import {} for-each block to adopt existing action pipelines
# (requires Terraform >= 1.11 for import for_each):
#
# import {
#   for_each = toset(data.datahub_action_pipelines.all.urns)
#   to       = datahub_action_pipeline.imported[each.value]
#   id       = each.value
# }
