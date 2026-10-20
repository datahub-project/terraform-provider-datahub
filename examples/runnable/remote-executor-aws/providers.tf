terraform {
  required_version = ">= 1.11"
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.27.0"
    }
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.69" # >= 6.59 required by the EKS module; write-only secret values need >= 5.88
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 3.4" # write-only secret data (data_wo) support
    }
    helm = {
      source  = "hashicorp/helm"
      version = "~> 3.3"
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

provider "aws" {
  region = var.region

  default_tags {
    tags = var.default_tags
  }
}

# The kubernetes and helm providers are configured from the EKS module's
# outputs. These values are unknown until the cluster exists, so Terraform
# defers planning of kubernetes/helm resources until the cluster is created
# within the same apply. Caveats:
#   - Authentication runs `aws eks get-token` at apply time, so the aws CLI
#     must be on PATH and see the same AWS credentials Terraform uses. The
#     token is short-lived and re-minted per call, so a long apply (the first
#     image pull is multi-gigabyte) never outlives it.
#   - If the cluster is replaced out-of-band, kubernetes/helm resources may
#     fail to refresh; remove them from state (terraform state rm) and
#     destroy/re-apply.
provider "kubernetes" {
  host                   = module.eks.cluster_endpoint
  cluster_ca_certificate = base64decode(module.eks.cluster_certificate_authority_data)

  exec {
    api_version = "client.authentication.k8s.io/v1beta1"
    command     = "aws"
    args        = ["eks", "get-token", "--cluster-name", module.eks.cluster_name, "--region", var.region]
  }
}

provider "helm" {
  kubernetes = {
    host                   = module.eks.cluster_endpoint
    cluster_ca_certificate = base64decode(module.eks.cluster_certificate_authority_data)

    exec = {
      api_version = "client.authentication.k8s.io/v1beta1"
      command     = "aws"
      args        = ["eks", "get-token", "--cluster-name", module.eks.cluster_name, "--region", var.region]
    }
  }
}
