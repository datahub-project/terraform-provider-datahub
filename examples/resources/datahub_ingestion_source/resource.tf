resource "datahub_ingestion_source" "example" {
  # source_id is optional; if omitted, it is derived from source_name
  # source_id   = "my-unity-source"
  source_name   = "My Unity Catalog Source"
  cron_interval = "0 10 * * *"
  timezone      = "UTC"
  cli_version   = "1.3.1.5"
  async         = false

  # source_type is optional; derived from recipe.source.type if omitted
  # source_type = "unity-catalog"

  recipe = jsonencode({
    source = {
      type = "unity-catalog"
      config = {
        workspace_url = var.databricks_workspace_url
        token         = var.databricks_pat
        env           = "PROD"
      }
    }
    pipeline_name = "unity-catalog:my-unity-source"
  })
}
