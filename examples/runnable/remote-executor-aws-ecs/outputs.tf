output "pool_id" {
  description = "Executor pool id the ECS task attaches to."
  value       = datahub_remote_executor_pool.ecs_fargate.pool_id
}

output "pool_urn" {
  description = "URN of the Remote Executor pool."
  value       = datahub_remote_executor_pool.ecs_fargate.urn
}

output "pool_state" {
  description = "Provisioning state of the pool (READY after a successful apply)."
  value       = datahub_remote_executor_pool.ecs_fargate.state_status
}

output "ingestion_source_urn" {
  description = "URN of the ingestion source pinned to the tf-example-ecs-fargate pool."
  value       = "urn:li:dataHubIngestionSource:${datahub_ingestion_source.s3.source_id}"
}

output "bucket_name" {
  description = "S3 bucket holding the seeded customers.csv object."
  value       = aws_s3_bucket.data.bucket
}

output "stack_name" {
  description = "CloudFormation stack running the Remote Executor."
  value       = aws_cloudformation_stack.executor.name
}

output "ecs_cluster_name" {
  description = "ECS cluster the stack created."
  value       = aws_cloudformation_stack.executor.outputs["ClusterName"]
}

output "ecs_service_name" {
  description = "ECS service the stack created."
  value       = aws_cloudformation_stack.executor.outputs["ServiceName"]
}

output "ecs_log_group_name" {
  description = "CloudWatch log group the executor writes to."
  value       = aws_cloudformation_stack.executor.outputs["ECSLogGroupName"]
}

output "ecs_logs_tail_command" {
  description = "Follow the executor's logs. Run with: eval \"$(terraform output -raw ecs_logs_tail_command)\""
  value       = "aws logs tail ${aws_cloudformation_stack.executor.outputs["ECSLogGroupName"]} --follow --region ${var.region}"
}

output "ecs_describe_service_command" {
  description = "Show the service's running and pending task counts and recent events."
  value       = "aws ecs describe-services --cluster ${aws_cloudformation_stack.executor.outputs["ClusterName"]} --services ${aws_cloudformation_stack.executor.outputs["ServiceName"]} --region ${var.region} --query 'services[0].{running:runningCount,pending:pendingCount,events:events[:5].message}'"
}

output "ecs_task_definition_secrets_command" {
  description = "List the names (never the values) of the Secrets Manager secrets injected into the executor container."
  value       = "aws ecs describe-task-definition --task-definition ${aws_cloudformation_stack.executor.name}-task --region ${var.region} --query 'taskDefinition.containerDefinitions[0].secrets[].name'"
}

output "restart_executor_command" {
  description = "Redeploy the executor so it picks up rotated secret values (environment-injected secrets are read once, at task start)."
  value       = "aws ecs update-service --cluster ${aws_cloudformation_stack.executor.outputs["ClusterName"]} --service ${aws_cloudformation_stack.executor.outputs["ServiceName"]} --force-new-deployment --region ${var.region}"
}

output "ecs_console_url" {
  description = "AWS Console page for the executor service (tasks, deployments, logs)."
  value       = "https://${var.region}.console.aws.amazon.com/ecs/v2/clusters/${aws_cloudformation_stack.executor.outputs["ClusterName"]}/services/${aws_cloudformation_stack.executor.outputs["ServiceName"]}/tasks?region=${var.region}"
}

output "remote_executors_url" {
  description = "DataHub UI page listing Remote Executor pools and their attached executors."
  value       = "${local.datahub_web_url}/ingestion/remote-executors?hideSystem=true&page=1"
}

output "ingestion_sources_url" {
  description = "DataHub UI page listing ingestion sources; use Run on the TF Example source to trigger a test ingest."
  value       = "${local.datahub_web_url}/ingestion/sources?hideSystem=true&page=1"
}

output "run_ingestion_command" {
  description = "Trigger the example ingestion source via the API (uses DATAHUB_GMS_TOKEN from your shell). Run with: eval \"$(terraform output -raw run_ingestion_command)\""
  value       = <<-EOT
    curl -sS -X POST "${local.datahub_web_url}/api/graphql" -H "Authorization: Bearer $DATAHUB_GMS_TOKEN" -H "Content-Type: application/json" -d '${jsonencode({ query = "mutation { createIngestionExecutionRequest(input: {ingestionSourceUrn: \"urn:li:dataHubIngestionSource:${datahub_ingestion_source.s3.source_id}\"}) }" })}'
  EOT
}

output "next_steps" {
  description = "How to verify the deployment and where to go next."
  value       = <<-EOT
    1. Check the service has one running task (the first image pull is multi-GB, be patient):
         eval "$(terraform output -raw ecs_describe_service_command)"
    2. Follow the executor's logs until it reports connecting to GMS:
         eval "$(terraform output -raw ecs_logs_tail_command)"
    3. Confirm the two Secrets Manager secrets are injected (names only):
         eval "$(terraform output -raw ecs_task_definition_secrets_command)"
    4. Confirm the executor attached to the pool:
         ${local.datahub_web_url}/ingestion/remote-executors?hideSystem=true&page=1
    5. Trigger the ingestion source and watch customers.csv become a dataset.
       UI (Run on "TF Example ECS - S3 CSV"):
         ${local.datahub_web_url}/ingestion/sources?hideSystem=true&page=1
       or API:
         eval "$(terraform output -raw run_ingestion_command)"
    6. Browse the service in the AWS Console (tasks, deployments, logs):
         https://${var.region}.console.aws.amazon.com/ecs/v2/clusters/${aws_cloudformation_stack.executor.outputs["ClusterName"]}/services/${aws_cloudformation_stack.executor.outputs["ServiceName"]}/tasks?region=${var.region}
    Remember: AWS resources bill until you run terraform destroy.
  EOT
}
