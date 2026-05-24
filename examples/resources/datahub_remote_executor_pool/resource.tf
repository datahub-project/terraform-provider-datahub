resource "datahub_remote_executor_pool" "analytics" {
  pool_id     = "analytics-team"
  description = "Pool for analytics-team Remote Executor workers running in private VPC"
}
