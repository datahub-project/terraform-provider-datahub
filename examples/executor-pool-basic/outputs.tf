output "pool_urn" {
  description = "The full DataHub URN of the created executor pool."
  value       = datahub_remote_executor_pool.analytics.urn
}

output "pool_id" {
  description = "The pool ID to set in your executor worker config (DATAHUB_EXECUTOR_POOL_ID)."
  value       = datahub_remote_executor_pool.analytics.pool_id
}

output "pool_state" {
  description = "Current provisioning state of the pool (PROVISIONING_PENDING -> READY)."
  value       = datahub_remote_executor_pool.analytics.state_status
}

output "helm_values_snippet" {
  description = "Copy-pasteable excerpt for your datahub-executor-worker Helm values.yaml."
  value       = "executorPoolId: ${datahub_remote_executor_pool.analytics.pool_id}"
}

output "ingestion_source_urn" {
  description = "The URN of the ingestion source configured to run on this pool."
  value       = "urn:li:dataHubIngestionSource:${datahub_ingestion_source.csv_enricher.source_id}"
}
