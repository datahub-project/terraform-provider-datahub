# The Remote Executor pool the ECS task attaches to. Deliberately not the
# default pool; the ingestion source references it explicitly. Create blocks
# until the pool reaches READY (typically 30-90 seconds).
resource "datahub_remote_executor_pool" "ecs_fargate" {
  pool_id     = local.pool_id
  description = "TF Example Remote Executor pool running on AWS ECS Fargate"
}

# DataHub-managed secret demonstrating the GMS-resolved secret path. The bucket
# name is not truly sensitive; it is stored as a secret so the recipe can
# exercise ${NAME} resolution via DataHub alongside the environment-injected
# Secrets Manager secrets.
resource "datahub_secret" "s3_bucket_name" {
  name             = "TF_EXAMPLE_ECS_S3_BUCKET_NAME"
  description      = "S3 bucket name for the TF Example ECS ingestion source"
  value            = aws_s3_bucket.data.bucket
  value_wo_version = 1
}

# Ingestion source pinned to the tf-example-ecs-fargate pool. The recipe
# references secrets resolved through two different mechanisms:
#   - ${TF_EXAMPLE_ECS_S3_BUCKET_NAME}: DataHub secret, resolved via GMS
#   - ${S3_ACCESS_KEY_ID} and ${S3_SECRET_ACCESS_KEY}: container environment
#     variables, injected by ECS from Secrets Manager at task start (the
#     template's OptionalSecrets parameter) and read by the executor's
#     environment secret store
# In HCL, secret references are written $${NAME} (double $) so Terraform
# passes the literal ${NAME} through instead of interpolating.
# No cron schedule: trigger runs manually from the DataHub Ingestion UI.
resource "datahub_ingestion_source" "s3" {
  # source_id is optional -- when omitted DataHub derives it from source_name
  # with a hash suffix. Setting it explicitly makes the URN deterministic, so
  # anything this example leaves behind names the example that created it.
  source_id          = "tf-example-ecs-s3"
  source_name        = "TF Example ECS - S3 CSV"
  remote_executor_id = datahub_remote_executor_pool.ecs_fargate.pool_id

  recipe = jsonencode({
    source = {
      type = "s3"
      config = {
        path_specs = [
          {
            include = "s3://$${TF_EXAMPLE_ECS_S3_BUCKET_NAME}/*.csv"
          }
        ]
        aws_config = {
          aws_access_key_id     = "$${${local.access_key_id_name}}"
          aws_secret_access_key = "$${${local.secret_access_key_name}}"
          aws_region            = var.region
        }
        env = "PROD"
      }
    }
    pipeline_name = "tf-example-ecs-s3"
  })

  depends_on = [datahub_secret.s3_bucket_name]
}
