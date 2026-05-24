# Look up the reserved default pool (auto-provisioned by DataHub Cloud).
data "datahub_remote_executor_pool" "default" {
  pool_id = "default"
}

# Route a private-network ingestion source through the default pool.
# A Postgres database inside your VPC is the canonical use case: DataHub
# Cloud cannot reach it directly, so ingestion must run on a Remote Executor
# deployed inside the same network.
resource "datahub_ingestion_source" "warehouse" {
  source_name        = "Warehouse Postgres"
  remote_executor_id = data.datahub_remote_executor_pool.default.pool_id
  recipe = jsonencode({
    source = {
      type = "postgres"
      config = {
        host_port = "postgres.internal:5432"
        database  = "warehouse"
        username  = "${POSTGRES_USER}"
        password  = "${POSTGRES_PASSWORD}"
      }
    }
  })
}
