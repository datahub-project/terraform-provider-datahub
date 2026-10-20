# Secrets Manager secret holding the ingest user's access key, file-mounted
# into the worker pods via the secrets-store CSI driver. One JSON document
# carries both halves of the key; the SecretProviderClass splits it into two
# files with jmesPath.
resource "aws_secretsmanager_secret" "s3_ingest_credentials" {
  name = "tf-example-aws-s3-ingest-credentials-${random_string.suffix.result}"
  # Deleted immediately on destroy instead of being scheduled for the default
  # 30-day recovery window, so the example can be re-applied without the name
  # colliding with a secret still pending deletion.
  recovery_window_in_days = 0
}

# Written with the write-only secret_string_wo attribute so the key never
# lands in this resource's state; if the access key is rotated, bump
# secret_string_wo_version to re-write.
resource "aws_secretsmanager_secret_version" "s3_ingest_credentials" {
  secret_id = aws_secretsmanager_secret.s3_ingest_credentials.id

  secret_string_wo = jsonencode({
    access_key_id     = aws_iam_access_key.ingest.id
    secret_access_key = aws_iam_access_key.ingest.secret
  })
  secret_string_wo_version = 1
}

# The role the CSI provider assumes, through EKS Pod Identity, when it mounts
# the secret into a worker pod. Pod Identity binds a role to a (namespace,
# service account) pair on the cluster, so the executor chart needs no IRSA
# annotation and the cluster needs no OIDC provider.
data "aws_iam_policy_document" "pod_identity_trust" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole", "sts:TagSession"]

    principals {
      type        = "Service"
      identifiers = ["pods.eks.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "executor_secrets" {
  name               = "tf-example-aws-executor-secrets-${random_string.suffix.result}"
  assume_role_policy = data.aws_iam_policy_document.pod_identity_trust.json
}

resource "aws_iam_role_policy" "executor_read_secret" {
  name = "read-s3-ingest-credentials"
  role = aws_iam_role.executor_secrets.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["secretsmanager:GetSecretValue", "secretsmanager:DescribeSecret"]
        Resource = aws_secretsmanager_secret.s3_ingest_credentials.arn
      },
    ]
  })
}

# The association only names the service account; the executor Helm release
# creates it. Ordering between the two does not matter to EKS, but the worker
# pod must start after the association exists or its first mount attempt
# fails, which helm_release.executor's depends_on enforces.
resource "aws_eks_pod_identity_association" "executor" {
  cluster_name    = module.eks.cluster_name
  namespace       = local.executor_namespace
  service_account = local.executor_service_account
  role_arn        = aws_iam_role.executor_secrets.arn
}
