output "connection_urn" {
  description = "Full DataHub URN of the Snowflake connection."
  value       = datahub_connection.snowflake.urn
}

output "next_steps" {
  description = "Post-apply summary and follow-up actions."
  value       = <<-EOT

  Snowflake connection and ingestion source created.

    Connection URN:  ${datahub_connection.snowflake.urn}
    Ingestion UI:    $DATAHUB_GMS_URL/ingestion

  The ingestion source recipe references the connection URN. When the
  executor runs the ingestion, it fetches and decrypts the Snowflake
  credentials from the connection blob -- no credentials appear in
  the recipe itself.

  To trigger ingestion immediately:
    Open $DATAHUB_GMS_URL/ingestion and click Run next to "${datahub_ingestion_source.snowflake.source_name}".

  To rotate credentials:
    1. Update snowflake_password in terraform.tfvars (or TF_VAR_snowflake_password).
    2. Increment config_wo_version in main.tf (e.g. 1 -> 2).
    3. terraform apply -- Terraform replaces the connection; the ingestion
       source URN reference is unchanged and continues to work.

  To remove:
    terraform destroy

  EOT
}
