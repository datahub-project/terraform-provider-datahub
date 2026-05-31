output "group_urn" {
  description = "Full URN of the created group."
  value       = datahub_corp_group.data_platform.urn
}

output "group_urn_via_lookup" {
  description = "Group URN resolved through the datahub_corp_group data source."
  value       = data.datahub_corp_group.data_platform.urn
}

output "member_user_urn" {
  description = "URN of the user added to the group."
  value       = data.datahub_corp_user.member.urn
}

output "assigned_role_urn" {
  description = "URN of the role assigned to the group."
  value       = datahub_role_assignment.data_platform_editor.role_urn
}

output "policy_urn" {
  description = "URN of the access policy granting the group its privileges."
  value       = datahub_policy.data_platform_admins.urn
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
