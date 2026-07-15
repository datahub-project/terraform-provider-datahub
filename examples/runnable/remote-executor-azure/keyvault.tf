# Key Vault holding the secret that is file-mounted into the worker pods via
# the AKS secrets-store CSI addon. Access-policy authorization is used instead
# of RBAC to avoid role-assignment propagation delays breaking a single apply.
resource "azurerm_key_vault" "kv" {
  name                       = "tfexdh-${random_string.suffix.result}"
  location                   = azurerm_resource_group.rg.location
  resource_group_name        = azurerm_resource_group.rg.name
  tenant_id                  = data.azurerm_client_config.current.tenant_id
  sku_name                   = "standard"
  soft_delete_retention_days = 7
  # Disabled so terraform destroy (with purge_soft_delete_on_destroy) fully
  # removes the vault and the example can be re-applied immediately.
  purge_protection_enabled   = false
  rbac_authorization_enabled = false
  tags                       = local.tags
}

# The identity running terraform needs to write and (on destroy) purge secrets.
resource "azurerm_key_vault_access_policy" "deployer" {
  key_vault_id = azurerm_key_vault.kv.id
  tenant_id    = data.azurerm_client_config.current.tenant_id
  object_id    = data.azurerm_client_config.current.object_id

  secret_permissions = ["Get", "List", "Set", "Delete", "Purge", "Recover"]
}

# The storage account key that the ingestion recipe consumes as the
# ${ABS_ACCOUNT_KEY} file secret. Written with the write-only value_wo
# attribute; if the storage key is rotated, bump value_wo_version to re-write.
resource "azurerm_key_vault_secret" "abs_account_key" {
  name             = "abs-account-key"
  key_vault_id     = azurerm_key_vault.kv.id
  value_wo         = azurerm_storage_account.data.primary_access_key
  value_wo_version = 1

  depends_on = [azurerm_key_vault_access_policy.deployer]
}

# The AKS secrets-store CSI addon identity reads secrets at pod mount time.
resource "azurerm_key_vault_access_policy" "csi_addon" {
  key_vault_id = azurerm_key_vault.kv.id
  tenant_id    = data.azurerm_client_config.current.tenant_id
  object_id    = azurerm_kubernetes_cluster.aks.key_vault_secrets_provider[0].secret_identity[0].object_id

  secret_permissions = ["Get"]
}
