# The Remote Executor pool the EKS workers attach to. Deliberately not the
# default pool; the ingestion source references it explicitly. Create blocks
# until the pool reaches READY (typically 30-90 seconds).
resource "datahub_remote_executor_pool" "aws_eks" {
  pool_id     = local.pool_id
  description = "TF Example Remote Executor pool running on AWS EKS"
}

# DataHub-managed secret demonstrating the GMS-resolved secret path. The bucket
# name is not truly sensitive; it is stored as a secret so the recipe can
# exercise ${NAME} resolution via DataHub alongside the file-mounted Secrets
# Manager secret.
resource "datahub_secret" "s3_bucket_name" {
  name             = "TF_EXAMPLE_AWS_S3_BUCKET_NAME"
  description      = "S3 bucket name for the TF Example S3 ingestion source"
  value            = aws_s3_bucket.data.bucket
  value_wo_version = 1
}

# Ingestion source pinned to the tf-example-aws-eks pool. The recipe references
# secrets resolved through two different mechanisms:
#   - ${TF_EXAMPLE_AWS_S3_BUCKET_NAME}: DataHub secret, resolved via GMS
#   - ${S3_ACCESS_KEY_ID} and ${S3_SECRET_ACCESS_KEY}: the two halves of the
#     Secrets Manager secret, file-mounted at /mnt/secrets/<name> by the
#     secrets-store CSI driver
# In HCL, secret references are written $${NAME} (double $) so Terraform
# passes the literal ${NAME} through instead of interpolating.
# No cron schedule: trigger runs manually from the DataHub Ingestion UI.
resource "datahub_ingestion_source" "s3" {
  # source_id is optional -- when omitted DataHub derives it from source_name
  # with a hash suffix. Setting it explicitly makes the URN deterministic, so
  # anything this example leaves behind names the example that created it.
  source_id          = "tf-example-aws-s3"
  source_name        = "TF Example AWS - S3 CSV"
  remote_executor_id = datahub_remote_executor_pool.aws_eks.pool_id

  recipe = jsonencode({
    source = {
      type = "s3"
      config = {
        path_specs = [
          {
            include = "s3://$${TF_EXAMPLE_AWS_S3_BUCKET_NAME}/*.csv"
          }
        ]
        aws_config = {
          aws_access_key_id     = "$${${local.access_key_id_alias}}"
          aws_secret_access_key = "$${${local.secret_access_key_alias}}"
          aws_region            = var.region
        }
        env = "PROD"
      }
    }
    pipeline_name = "tf-example-aws-s3"
  })

  depends_on = [datahub_secret.s3_bucket_name]
}
