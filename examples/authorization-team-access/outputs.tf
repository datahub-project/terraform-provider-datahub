output "group_urn" {
  description = "Full URN of the created group."
  value       = datahub_corp_group.data_platform.urn
}

output "group_urn_via_lookup" {
  description = "Group URN resolved through the datahub_corp_group data source."
  value       = data.datahub_corp_group.data_platform.urn
}

output "next_steps" {
  description = "Post-apply summary."
  value       = <<-EOT

  Group created: ${datahub_corp_group.data_platform.urn}

    Verify in the DataHub UI:
      $DATAHUB_GMS_URL/settings/identities/groups

  To remove all resources:
    terraform destroy

  EOT
}
