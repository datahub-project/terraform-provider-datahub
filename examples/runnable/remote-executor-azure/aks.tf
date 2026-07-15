# A small but not-toy AKS cluster: two worker nodes, system-assigned identity,
# and the secrets-store CSI driver addon with the Azure Key Vault provider.
# Everything else is left at AKS defaults to keep the example minimal.
resource "azurerm_kubernetes_cluster" "aks" {
  name                = "tf-example-datahub-aks"
  location            = azurerm_resource_group.rg.location
  resource_group_name = azurerm_resource_group.rg.name
  dns_prefix          = "tfexdh${random_string.suffix.result}"

  default_node_pool {
    name       = "system"
    node_count = var.node_count
    vm_size    = var.node_vm_size
  }

  identity {
    type = "SystemAssigned"
  }

  # Enables the secrets-store CSI driver + Azure Key Vault provider, used to
  # file-mount the abs-account-key secret into the worker pods.
  key_vault_secrets_provider {
    secret_rotation_enabled = true
  }

  tags = local.tags
}
