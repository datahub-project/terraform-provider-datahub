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
# kubeconfig. These values are unknown until the cluster exists, so Terraform
# defers planning of kubernetes/helm resources until the cluster is created
# within the same apply. Caveats:
#   - AKS local accounts must remain enabled (they are by default).
#   - If the cluster is replaced out-of-band, kubernetes/helm resources may
#     fail to refresh; remove them from state (terraform state rm) and
#     destroy/re-apply.
#
# Credentials are parsed from kube_config_raw rather than the structured
# kube_config attributes: when the AKS user credential contains both client
# certificates and a token, azurerm's parsed kube_config[0].client_certificate
# and client_key come back empty (observed with azurerm 4.81, July 2026) and
# the Kubernetes API rejects the resulting anonymous requests with
# "Unauthorized". The raw kubeconfig always carries the certificates.
locals {
  kubeconfig = yamldecode(azurerm_kubernetes_cluster.aks.kube_config_raw)

  kube_host        = local.kubeconfig.clusters[0].cluster.server
  kube_ca          = base64decode(local.kubeconfig.clusters[0].cluster["certificate-authority-data"])
  kube_client_cert = base64decode(local.kubeconfig.users[0].user["client-certificate-data"])
  kube_client_key  = base64decode(local.kubeconfig.users[0].user["client-key-data"])
}

provider "kubernetes" {
  host                   = local.kube_host
  client_certificate     = local.kube_client_cert
  client_key             = local.kube_client_key
  cluster_ca_certificate = local.kube_ca
}

provider "helm" {
  kubernetes = {
    host                   = local.kube_host
    client_certificate     = local.kube_client_cert
    client_key             = local.kube_client_key
    cluster_ca_certificate = local.kube_ca
  }
}
