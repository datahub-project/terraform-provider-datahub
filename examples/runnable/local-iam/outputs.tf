output "group_urn" {
  description = "Full URN of the created group."
  value       = datahub_corp_group.data_platform.urn
}

output "group_urn_via_lookup" {
  description = "Group URN resolved through the datahub_corp_group data source."
  value       = data.datahub_corp_group.data_platform.urn
}

output "team_member_urn" {
  description = "URN of the newly created login user. On Cloud this is urn:li:corpuser:<email>; on OSS it is urn:li:corpuser:<username>."
  value       = datahub_local_user_login.team_member.user_urn
}

output "team_member_reset_url" {
  description = "Single-use 24h password reset link for the new team member. Send this to the user so they can set their own password."
  value       = datahub_local_user_login.team_member.password_reset_url
  sensitive   = true
}

output "pipeline_bot_urn" {
  description = "URN of the catalog-only pipeline bot service account."
  value       = datahub_corp_user.pipeline_bot.urn
}

output "existing_member_urn" {
  description = "URN of the pre-existing user added to the group."
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
  description = "Post-apply summary with onboarding instructions."
  value       = <<-EOT

  Group created: ${datahub_corp_group.data_platform.urn}

  New login user: ${datahub_local_user_login.team_member.user_urn}
    Send the reset link to the user (expires in 24h):
      terraform output -raw team_member_reset_url

  Pipeline bot: ${datahub_corp_user.pipeline_bot.urn}
    (catalog record only -- no login credentials)

  Verify in the DataHub UI:
    $DATAHUB_GMS_URL/settings/identities/groups
    $DATAHUB_GMS_URL/settings/identities/users

  To remove all resources:
    terraform destroy
    (Warning: destroys the group, all memberships, and the created users)

  EOT
}
