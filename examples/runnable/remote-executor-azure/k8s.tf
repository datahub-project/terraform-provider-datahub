resource "kubernetes_namespace_v1" "executor" {
  metadata {
    name = local.executor_namespace
  }
}

# DataHub access token the workers use to authenticate to GMS. Secret name and
# key match the chart defaults (global.datahub.gms.secretRef / secretKey).
# Written with the write-only data_wo attribute so the token never lands in
# Terraform state; bump data_wo_revision after rotating the token.
resource "kubernetes_secret_v1" "datahub_pat" {
  metadata {
    name      = "datahub-access-token-secret"
    namespace = kubernetes_namespace_v1.executor.metadata[0].name
  }

  data_wo = {
    "datahub-access-token-secret-key" = var.datahub_executor_token
  }
  data_wo_revision = 1
}

# Image pull secret for the Remote Executor registry (Cloudsmith). Also
# write-only; bump data_wo_revision after rotating the entitlement token.
resource "kubernetes_secret_v1" "cloudsmith_pull" {
  metadata {
    name      = "datahub-executor-pull"
    namespace = kubernetes_namespace_v1.executor.metadata[0].name
  }

  type = "kubernetes.io/dockerconfigjson"

  data_wo = {
    ".dockerconfigjson" = jsonencode({
      auths = {
        (var.cloudsmith_registry) = {
          username = var.cloudsmith_username
          password = var.cloudsmith_token
          auth     = base64encode("${var.cloudsmith_username}:${var.cloudsmith_token}")
        }
      }
    })
  }
  data_wo_revision = 1
}

# SecretProviderClass wiring the Key Vault secret to a pod-mountable CSI
# volume. Delivered via a minimal local chart because kubernetes_manifest
# cannot plan a custom resource before the cluster (and the CRD installed by
# the AKS addon) exists; helm renders at apply time only.
resource "helm_release" "executor_support" {
  name      = "executor-support"
  chart     = "${path.module}/charts/executor-support"
  namespace = kubernetes_namespace_v1.executor.metadata[0].name

  values = [
    yamlencode({
      keyVaultName           = azurerm_key_vault.kv.name
      tenantId               = data.azurerm_client_config.current.tenant_id
      userAssignedIdentityId = azurerm_kubernetes_cluster.aks.key_vault_secrets_provider[0].secret_identity[0].client_id
    })
  ]

  depends_on = [
    azurerm_key_vault_access_policy.csi_addon,
    azurerm_key_vault_secret.abs_account_key,
  ]
}

# The Remote Executor workers. Referencing the pool's pool_id means workers
# only start after the pool is READY, and are torn down before the pool on
# destroy.
resource "helm_release" "executor" {
  name       = "datahub-executor"
  repository = "https://executor-helm.acryl.io"
  chart      = "datahub-executor-worker"
  version    = var.executor_chart_version
  namespace  = kubernetes_namespace_v1.executor.metadata[0].name
  timeout    = 600
  wait       = true

  values = [
    yamlencode({
      replicaCount = var.executor_replica_count

      image = {
        repository = "${var.cloudsmith_registry}/${var.executor_image_repository}"
        tag        = var.executor_image_tag
      }

      imagePullSecrets = [
        { name = kubernetes_secret_v1.cloudsmith_pull.metadata[0].name }
      ]

      global = {
        datahub = {
          executor = {
            pool_id = datahub_remote_executor_pool.azure_aks.pool_id
          }
          gms = {
            url = var.datahub_gms_url
          }
        }
      }

      # Sole deviation from chart defaults: see var.executor_cpu_request.
      resources = {
        requests = {
          cpu    = var.executor_cpu_request
          memory = "8Gi"
        }
      }

      extraVolumes = [
        {
          name = "kv-secrets"
          csi = {
            driver   = "secrets-store.csi.k8s.io"
            readOnly = true
            volumeAttributes = {
              secretProviderClass = "datahub-executor-kv"
            }
          }
        }
      ]

      # Files land flat under /mnt/secrets; the executor resolves
      # ${ABS_ACCOUNT_KEY} in recipes from /mnt/secrets/ABS_ACCOUNT_KEY.
      extraVolumeMounts = [
        {
          name      = "kv-secrets"
          mountPath = "/mnt/secrets"
          readOnly  = true
        }
      ]
    })
  ]

  depends_on = [
    helm_release.executor_support,
    kubernetes_secret_v1.datahub_pat,
  ]
}
