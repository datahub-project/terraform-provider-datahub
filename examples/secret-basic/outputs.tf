output "secret_urn" {
  description = "Full DataHub URN of the secret. Use this as $${TF_EXAMPLE_SECRET} in other recipes."
  value       = datahub_secret.example_secret.urn
}

output "ingestion_source_id" {
  description = "Short ID of the ingestion source."
  value       = datahub_ingestion_source.example.source_id
}

output "next_steps" {
  description = "Post-apply summary."
  value       = <<-EOT

  Secret and ingestion source created.

    Secret URN:  ${datahub_secret.example_secret.urn}
    Source ID:   ${datahub_ingestion_source.example.source_id}
    DataHub UI:  $DATAHUB_GMS_URL/settings/secrets  (view secret)
                 $DATAHUB_GMS_URL/ingestion         (view source)

  The secret value is encrypted in DataHub and not stored in terraform.tfstate.
  Run `terraform state show datahub_secret.example_secret` to confirm that
  `value` is null in state.

  To rotate the secret:
    1. Update the value (e.g. change TF_VAR_secret_value).
    2. Increment value_wo_version in main.tf (e.g. 1 -> 2).
    3. terraform apply  -- Terraform plans a replacement of the secret.

  To remove all resources:
    terraform destroy

  EOT
}
