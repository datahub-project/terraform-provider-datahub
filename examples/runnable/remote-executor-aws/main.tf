locals {
  # Fixed pool id; the executor pool this example demonstrates is deliberately
  # NOT the default pool, so the ingestion source must reference it explicitly.
  pool_id = "tf-example-aws-eks"

  cluster_name       = "tf-example-datahub-eks"
  executor_namespace = "datahub-executor"

  # The executor chart does not create a service account by default (pods run
  # as "default"). The example has it create one under a fixed name so the EKS
  # Pod Identity association can target it without a service-account
  # annotation.
  executor_service_account = "datahub-executor"

  # S3 bucket names are global, so the seeded bucket takes a random suffix.
  bucket_name = "tf-example-aws-data-${random_string.suffix.result}"

  # Filenames the two halves of the access key are mounted under in the worker
  # pods (/mnt/secrets/<name>). The ingestion recipe references them as
  # ${S3_ACCESS_KEY_ID} and ${S3_SECRET_ACCESS_KEY}; the names are deliberately
  # not AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY so they cannot be mistaken for
  # the SDK's environment variables.
  access_key_id_alias     = "S3_ACCESS_KEY_ID"
  secret_access_key_alias = "S3_SECRET_ACCESS_KEY"

  # Web UI base URL: the GMS URL without its /gms suffix.
  datahub_web_url = trimsuffix(var.datahub_gms_url, "/gms")
}

# Suffix for globally-unique AWS names (S3 bucket, Secrets Manager secret).
resource "random_string" "suffix" {
  length  = 6
  lower   = true
  numeric = true
  upper   = false
  special = false
}

data "aws_caller_identity" "current" {}

data "aws_availability_zones" "available" {
  state = "available"
}
