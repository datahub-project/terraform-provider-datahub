terraform {
  required_version = ">= 1.11"
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.15.0"
    }
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 4.30" # >= 4.23 required for write-only key vault secret values
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.38" # write-only secret data (data_wo) support
    }
    helm = {
      source  = "hashicorp/helm"
      version = "~> 3.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}

provider "datahub" {
  # Credentials from environment:
  #   DATAHUB_GMS_URL   - e.g. https://your-instance.acryl.io
  #   DATAHUB_GMS_TOKEN - personal access token
}

provider "azurerm" {
  features {
    key_vault {
      # Fully purge the vault on destroy so the example can be re-applied
      # without colliding with a soft-deleted vault of the same name.
      purge_soft_delete_on_destroy    = true
      recover_soft_deleted_key_vaults = false
    }
    resource_group {
      prevent_deletion_if_contains_resources = false
    }
  }
}

# The kubernetes and helm providers are configured from the AKS cluster's
# admin kubeconfig. These values are unknown until the cluster exists, so
# Terraform defers planning of kubernetes/helm resources until the cluster
# is created within the same apply. Caveats:
#   - AKS local accounts must remain enabled (they are by default).
#   - If the cluster is replaced out-of-band, kubernetes/helm resources may
#     fail to refresh; remove them from state (terraform state rm) and
#     destroy/re-apply.
provider "kubernetes" {
  host                   = azurerm_kubernetes_cluster.aks.kube_config[0].host
  client_certificate     = base64decode(azurerm_kubernetes_cluster.aks.kube_config[0].client_certificate)
  client_key             = base64decode(azurerm_kubernetes_cluster.aks.kube_config[0].client_key)
  cluster_ca_certificate = base64decode(azurerm_kubernetes_cluster.aks.kube_config[0].cluster_ca_certificate)
}

provider "helm" {
  kubernetes = {
    host                   = azurerm_kubernetes_cluster.aks.kube_config[0].host
    client_certificate     = base64decode(azurerm_kubernetes_cluster.aks.kube_config[0].client_certificate)
    client_key             = base64decode(azurerm_kubernetes_cluster.aks.kube_config[0].client_key)
    cluster_ca_certificate = base64decode(azurerm_kubernetes_cluster.aks.kube_config[0].cluster_ca_certificate)
  }
}
