output "pool_id" {
  description = "Executor pool id the workers attach to."
  value       = datahub_remote_executor_pool.azure_aks.pool_id
}

output "pool_urn" {
  description = "URN of the Remote Executor pool."
  value       = datahub_remote_executor_pool.azure_aks.urn
}

output "pool_state" {
  description = "Provisioning state of the pool (READY after a successful apply)."
  value       = datahub_remote_executor_pool.azure_aks.state_status
}

output "ingestion_source_urn" {
  description = "URN of the ingestion source pinned to the azure-aks pool."
  value       = "urn:li:dataHubIngestionSource:${datahub_ingestion_source.abs.source_id}"
}

output "storage_account_name" {
  description = "Storage account holding the seeded customers.csv blob."
  value       = azurerm_storage_account.data.name
}

output "key_vault_name" {
  description = "Key Vault holding the file-mounted abs-account-key secret."
  value       = azurerm_key_vault.kv.name
}

output "aks_get_credentials_command" {
  description = "Fetch kubeconfig credentials for the example cluster."
  value       = "az aks get-credentials --resource-group ${azurerm_resource_group.rg.name} --name ${azurerm_kubernetes_cluster.aks.name} --overwrite-existing"
}

output "aks_portal_url" {
  description = "Azure Portal page for the cluster (Kubernetes resources section shows workloads, pods, events, and live logs)."
  value       = "https://portal.azure.com/#resource${azurerm_kubernetes_cluster.aks.id}/overview"
}

output "remote_executors_url" {
  description = "DataHub UI page listing Remote Executor pools and their attached workers."
  value       = "${local.datahub_web_url}/ingestion/remote-executors?hideSystem=true&page=1"
}

output "ingestion_sources_url" {
  description = "DataHub UI page listing ingestion sources; use Run on the TF Example source to trigger a test ingest."
  value       = "${local.datahub_web_url}/ingestion/sources?hideSystem=true&page=1"
}

output "run_ingestion_command" {
  description = "Trigger the example ingestion source via the API (uses DATAHUB_GMS_TOKEN from your shell). Run with: eval \"$(terraform output -raw run_ingestion_command)\""
  value       = <<-EOT
    curl -sS -X POST "${local.datahub_web_url}/api/graphql" -H "Authorization: Bearer $DATAHUB_GMS_TOKEN" -H "Content-Type: application/json" -d '${jsonencode({ query = "mutation { createIngestionExecutionRequest(input: {ingestionSourceUrn: \"urn:li:dataHubIngestionSource:${datahub_ingestion_source.abs.source_id}\"}) }" })}'
  EOT
}

output "next_steps" {
  description = "How to verify the deployment and where to go next."
  value       = <<-EOT
    1. Fetch cluster credentials:
         eval "$(terraform output -raw aks_get_credentials_command)"
    2. Check the worker pod is Running (first image pull is multi-GB, be patient):
         kubectl -n ${local.executor_namespace} get pods
    3. Confirm the Key Vault secret is file-mounted:
         kubectl -n ${local.executor_namespace} exec "$(kubectl -n ${local.executor_namespace} get pod -o name | head -1)" -- ls /mnt/secrets
    4. Confirm the worker attached to the pool:
         ${local.datahub_web_url}/ingestion/remote-executors?hideSystem=true&page=1
    5. Trigger the ingestion source and watch customers.csv become a dataset.
       UI (Run on "TF Example Azure Blob CSV (azure-aks pool)"):
         ${local.datahub_web_url}/ingestion/sources?hideSystem=true&page=1
       or API:
         eval "$(terraform output -raw run_ingestion_command)"
    6. Browse the cluster in the Azure Portal (workloads, pods, live logs):
         https://portal.azure.com/#resource${azurerm_kubernetes_cluster.aks.id}/overview
    Remember: Azure resources bill until you run terraform destroy.
  EOT
}
