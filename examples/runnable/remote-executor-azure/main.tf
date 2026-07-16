locals {
  tags = var.default_tags

  # Fixed pool id; the executor pool this example demonstrates is deliberately
  # NOT the default pool, so the ingestion source must reference it explicitly.
  pool_id = "azure-aks"

  executor_namespace = "datahub-executor"
  container_name     = "tf-example-data"

  # Web UI base URL: the GMS URL without its /gms suffix.
  datahub_web_url = trimsuffix(var.datahub_gms_url, "/gms")
}

# Suffix for globally-unique Azure names (key vault, storage account).
resource "random_string" "suffix" {
  length  = 6
  lower   = true
  numeric = true
  upper   = false
  special = false
}

data "azurerm_client_config" "current" {}

resource "azurerm_resource_group" "rg" {
  name     = var.resource_group_name
  location = var.location
  tags     = local.tags
}
