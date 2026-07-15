# The Remote Executor pool the AKS workers attach to. Deliberately not the
# default pool; the ingestion source references it explicitly. Create blocks
# until the pool reaches READY (typically 30-90 seconds).
resource "datahub_remote_executor_pool" "azure_aks" {
  pool_id     = local.pool_id
  description = "TF Example Remote Executor pool running on Azure AKS"
}

# DataHub-managed secret demonstrating the GMS-resolved secret path. The
# storage account name is not truly sensitive; it is stored as a secret so the
# recipe can exercise ${NAME} resolution via DataHub alongside the
# file-mounted Key Vault secret.
resource "datahub_secret" "abs_account_name" {
  name             = "TF_EXAMPLE_ABS_ACCOUNT_NAME"
  description      = "Azure storage account name for the TF Example ABS ingestion source"
  value            = azurerm_storage_account.data.name
  value_wo_version = 1
}

# Ingestion source pinned to the azure-aks pool. The recipe references two
# secrets resolved through different mechanisms:
#   - ${TF_EXAMPLE_ABS_ACCOUNT_NAME}: DataHub secret, resolved via GMS
#   - ${ABS_ACCOUNT_KEY}: Key Vault secret, file-mounted at
#     /mnt/secrets/ABS_ACCOUNT_KEY by the secrets-store CSI driver
# In HCL, secret references are written $${NAME} (double $) so Terraform
# passes the literal ${NAME} through instead of interpolating.
# No cron schedule: trigger runs manually from the DataHub Ingestion UI.
resource "datahub_ingestion_source" "abs" {
  source_name        = "TF Example Azure Blob CSV (azure-aks pool)"
  remote_executor_id = datahub_remote_executor_pool.azure_aks.pool_id

  recipe = jsonencode({
    source = {
      type = "abs"
      config = {
        path_specs = [
          {
            include = "https://$${TF_EXAMPLE_ABS_ACCOUNT_NAME}.blob.core.windows.net/${local.container_name}/*.csv"
          }
        ]
        azure_config = {
          account_name   = "$${TF_EXAMPLE_ABS_ACCOUNT_NAME}"
          account_key    = "$${ABS_ACCOUNT_KEY}"
          container_name = local.container_name
        }
        env = "PROD"
      }
    }
    pipeline_name = "tf-example-abs-azure-aks"
  })

  depends_on = [datahub_secret.abs_account_name]
}
