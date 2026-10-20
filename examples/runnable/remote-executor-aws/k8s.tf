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

# Secrets Store CSI driver plus its AWS provider. On AKS this is a cluster
# addon flag; on EKS it is a chart. The AWS provider chart installs the
# upstream secrets-store-csi-driver as a subchart, so one release covers both.
resource "helm_release" "secrets_store_csi" {
  name       = "secrets-store-csi"
  repository = "https://aws.github.io/secrets-store-csi-driver-provider-aws"
  chart      = "secrets-store-csi-driver-provider-aws"
  version    = var.csi_provider_chart_version
  namespace  = "kube-system"
  wait       = true

  values = [
    yamlencode({
      "secrets-store-csi-driver" = {
        enableSecretRotation = true
      }
    })
  ]

  depends_on = [module.eks]
}

# SecretProviderClass wiring the Secrets Manager secret to a pod-mountable CSI
# volume. Delivered via a minimal local chart because kubernetes_manifest
# cannot plan a custom resource before the cluster (and the CRD installed by
# the driver chart) exists; helm renders at apply time only.
resource "helm_release" "executor_support" {
  name      = "executor-support"
  chart     = "${path.module}/charts/executor-support"
  namespace = kubernetes_namespace_v1.executor.metadata[0].name

  values = [
    yamlencode({
      secretArn            = aws_secretsmanager_secret.s3_ingest_credentials.arn
      accessKeyIdAlias     = local.access_key_id_alias
      secretAccessKeyAlias = local.secret_access_key_alias
    })
  ]

  depends_on = [
    helm_release.secrets_store_csi,
    aws_secretsmanager_secret_version.s3_ingest_credentials,
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

      # The chart's default is create: false (pods run as "default"). A named
      # service account is what the Pod Identity association binds the
      # Secrets Manager role to.
      serviceAccount = {
        create = true
        name   = local.executor_service_account
      }

      global = {
        datahub = {
          executor = {
            pool_id = datahub_remote_executor_pool.aws_eks.pool_id
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
          name = "secrets-manager"
          csi = {
            driver   = "secrets-store.csi.k8s.io"
            readOnly = true
            volumeAttributes = {
              secretProviderClass = "datahub-executor-secrets"
            }
          }
        }
      ]

      # Files land flat under /mnt/secrets; the executor resolves
      # ${S3_ACCESS_KEY_ID} in recipes from /mnt/secrets/S3_ACCESS_KEY_ID.
      extraVolumeMounts = [
        {
          name      = "secrets-manager"
          mountPath = "/mnt/secrets"
          readOnly  = true
        }
      ]
    })
  ]

  depends_on = [
    helm_release.executor_support,
    kubernetes_secret_v1.datahub_pat,
    aws_eks_pod_identity_association.executor,
  ]
}
