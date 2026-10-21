# The published Remote Executor CloudFormation template, fetched at plan time
# from the acryldata/datahub-cloudformation repository at a pinned commit.
# Fetched rather than copied into this repository because that repository
# carries no licence, so there is no grant to redistribute the template here;
# and pinned by commit because it has no release tags and master moves roughly
# weekly with image-tag bumps. The 36 KB template is well under the 51,200-byte
# template_body limit, and template_url would need an S3 location.
data "http" "executor_template" {
  url = "https://raw.githubusercontent.com/acryldata/datahub-cloudformation/${var.template_git_ref}/remote-executor/datahub-executor.ecs.template.yaml"

  retry {
    attempts = 3
  }

  # The http provider does not fail on a non-2xx response; without this guard
  # a "404: Not Found" body would be submitted as the template and rejected by
  # CloudFormation with an opaque validation error.
  lifecycle {
    postcondition {
      condition     = self.status_code == 200 && startswith(self.response_body, "AWSTemplateFormatVersion")
      error_message = "Fetching the Remote Executor template returned HTTP ${self.status_code}; check template_git_ref names a commit that contains remote-executor/datahub-executor.ecs.template.yaml."
    }
  }
}

# The Remote Executor itself: an ECS cluster, Fargate task definition and
# service, IAM roles, log group and security group, all created by the
# template. Referencing the pool's pool_id means the stack is created only
# after the pool is READY, and torn down before the pool on destroy.
resource "aws_cloudformation_stack" "executor" {
  name          = local.stack_name
  template_body = data.http.executor_template.response_body

  # NAMED_IAM: the template gives both roles fixed names. AUTO_EXPAND: the
  # template declares Transform: AWS::LanguageExtensions (for Fn::Length).
  capabilities = ["CAPABILITY_NAMED_IAM", "CAPABILITY_AUTO_EXPAND"]

  parameters = {
    SubnetID       = module.vpc.private_subnets[0]
    VPCID          = module.vpc.vpc_id
    DataHubBaseUrl = var.datahub_gms_url

    # The token secret is owned by Terraform (see secretsmanager.tf), so the
    # template's NoEcho DataHubAccessToken parameter is never set and the
    # template creates no secret of its own.
    ExistingDataHubAccessTokenSecretArn = aws_secretsmanager_secret.executor_token.arn

    ExecutorPoolId = datahub_remote_executor_pool.ecs_fargate.pool_id

    # Becomes the container's AWS_REGION. The template's own text is ambiguous
    # about what this is for (its label mentions a queue, its description the
    # executor's region); the deployment region is the sensible value and the
    # one the worker attached with in testing.
    AwsRegion = var.region

    # Map values cannot be null, so the tag is always passed explicitly even
    # though it duplicates the template default at the pinned commit.
    ImageTag     = var.executor_image_tag
    DesiredCount = tostring(var.executor_desired_count)

    # The template defaults this to 21 GiB, a hair above the 20 GiB that
    # evicted the executor on EKS (multi-gigabyte image plus the plugin venvs
    # it installs on local disk). Fargate bills only the GiB above 20.
    TaskEphemeralStorageSizeInGiB = tostring(var.executor_ephemeral_storage_gib)

    # NAME=secretArn pairs, comma-separated, no spaces. Each becomes a container
    # environment variable injected from Secrets Manager at task start, and the
    # execution role is granted GetSecretValue on each ARN. join() rather than
    # a literal so there is never a trailing comma: an empty element makes the
    # template's Fn::Select fail at create time.
    OptionalSecrets = join(",", [
      "${local.access_key_id_name}=${aws_secretsmanager_secret.s3_access_key_id.arn}",
      "${local.secret_access_key_name}=${aws_secretsmanager_secret.s3_secret_access_key.arn}",
    ])
  }

  # Create is dominated by the image pull and the service reaching a steady
  # state; delete by the service draining. Both comfortably fit the provider's
  # 30-minute defaults, which are stated here so nobody has to look them up.
  timeouts {
    create = "30m"
    delete = "30m"
  }

  # The parameter references above already order the stack after the pool and
  # the secret *shells*. The task also needs the secret *versions* to exist
  # (or its first start fails with "unable to pull secrets") and the private
  # subnet's route to the NAT gateway (or it cannot reach ECR). Neither is a
  # parameter, so both are stated here.
  depends_on = [
    module.vpc,
    aws_secretsmanager_secret_version.executor_token,
    aws_secretsmanager_secret_version.s3_access_key_id,
    aws_secretsmanager_secret_version.s3_secret_access_key,
  ]
}
