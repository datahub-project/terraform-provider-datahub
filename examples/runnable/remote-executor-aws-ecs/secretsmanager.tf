# Three Secrets Manager secrets, all consumed by the ECS task definition the
# CloudFormation stack creates: the executor's DataHub token, and the two
# halves of the ingest user's access key. Each is a plain string, not a JSON
# document: the template grants the execution role GetSecretValue on the exact
# ARN it is given, and the ":json-key::" selector ECS would need to pick one
# key out of a document is part of that string, so a selector-suffixed ARN
# matches no secret in the policy and the task fails to start.
#
# All three are written with the write-only secret_string_wo attribute so no
# value lands in this resource's state; bump the matching *_wo_version to
# re-write after a rotation. recovery_window_in_days = 0 deletes immediately
# on destroy instead of scheduling a 30-day recovery window, so the example
# can be re-applied without the names colliding with secrets pending deletion.

# The Remote Executor token, injected as DATAHUB_GMS_TOKEN. Owned here rather
# than passed to the template's DataHubAccessToken parameter: the stack
# resource has no write-only parameters, and a NoEcho parameter reads back as
# "****", which would both put the token in state and show as permanent drift.
resource "aws_secretsmanager_secret" "executor_token" {
  name                    = "tf-example-ecs-executor-token-${random_string.suffix.result}"
  recovery_window_in_days = 0
}

resource "aws_secretsmanager_secret_version" "executor_token" {
  secret_id                = aws_secretsmanager_secret.executor_token.id
  secret_string_wo         = var.datahub_executor_token
  secret_string_wo_version = 1
}

resource "aws_secretsmanager_secret" "s3_access_key_id" {
  name                    = "tf-example-ecs-s3-access-key-id-${random_string.suffix.result}"
  recovery_window_in_days = 0
}

resource "aws_secretsmanager_secret_version" "s3_access_key_id" {
  secret_id                = aws_secretsmanager_secret.s3_access_key_id.id
  secret_string_wo         = aws_iam_access_key.ingest.id
  secret_string_wo_version = 1
}

resource "aws_secretsmanager_secret" "s3_secret_access_key" {
  name                    = "tf-example-ecs-s3-secret-access-key-${random_string.suffix.result}"
  recovery_window_in_days = 0
}

resource "aws_secretsmanager_secret_version" "s3_secret_access_key" {
  secret_id                = aws_secretsmanager_secret.s3_secret_access_key.id
  secret_string_wo         = aws_iam_access_key.ingest.secret
  secret_string_wo_version = 1
}
