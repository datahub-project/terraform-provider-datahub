locals {
  # Fixed pool id; the executor pool this example demonstrates is deliberately
  # NOT the default pool, so the ingestion source must reference it explicitly.
  pool_id = "tf-example-ecs-fargate"

  # Every fixed name the CloudFormation template creates (cluster, service, log
  # group, IAM roles, security group) derives from the stack name. IAM role
  # names are global to the account, so one stack name per account.
  stack_name = "tf-example-datahub-ecs"

  # S3 bucket names are global, so the seeded bucket takes a random suffix.
  bucket_name = "tf-example-ecs-data-${random_string.suffix.result}"

  # Names of the two container environment variables ECS injects from Secrets
  # Manager. The ingestion recipe references them as ${S3_ACCESS_KEY_ID} and
  # ${S3_SECRET_ACCESS_KEY}; the names are deliberately not AWS_ACCESS_KEY_ID /
  # AWS_SECRET_ACCESS_KEY so they cannot be mistaken for (or picked up as) the
  # SDK's own credential variables.
  access_key_id_name     = "S3_ACCESS_KEY_ID"
  secret_access_key_name = "S3_SECRET_ACCESS_KEY"

  # Web UI base URL: the GMS URL without its /gms suffix.
  datahub_web_url = trimsuffix(var.datahub_gms_url, "/gms")
}

# Suffix for globally-unique AWS names (S3 bucket, Secrets Manager secrets).
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
