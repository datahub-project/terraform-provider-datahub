# Look up the reserved default pool (auto-provisioned by DataHub Cloud).
data "datahub_remote_executor_pool" "default" {
  pool_id = "default"
}

# Reference the default pool's ID in an ingestion source.
resource "datahub_ingestion_source" "bigquery" {
  source_name        = "BigQuery Production"
  remote_executor_id = data.datahub_remote_executor_pool.default.pool_id
  recipe = jsonencode({
    source = {
      type = "bigquery"
    }
  })
}
