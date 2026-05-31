output "connection_urn" {
  description = "Full DataHub URN of the Snowflake connection. Reference this in ingestion source recipes via the connection field."
  value       = datahub_connection.snowflake.urn
}

output "next_steps" {
  description = "Post-apply summary and follow-up actions."
  value       = <<-EOT

  Snowflake connection created.

    URN:  ${datahub_connection.snowflake.urn}
    UI:   $DATAHUB_GMS_URL/settings/integrations

  Verify: the connection should appear as a Snowflake card in
  Settings > Integrations in the DataHub Cloud UI.

  To reference this connection in an ingestion source recipe, add:

    connection: ${datahub_connection.snowflake.urn}

  To rotate the password:
    1. Update snowflake_password in terraform.tfvars (or TF_VAR_snowflake_password).
    2. Increment config_wo_version in main.tf (e.g. 1 -> 2).
    3. terraform apply -- Terraform plans a destroy-before-create replacement.
       The URN is unchanged after rotation; any recipes referencing it
       continue to work without modification.

  To remove:
    terraform destroy

  EOT
}
