output "pool_id" {
  description = "Executor pool id the workers attach to."
  value       = datahub_remote_executor_pool.aws_eks.pool_id
}

output "pool_urn" {
  description = "URN of the Remote Executor pool."
  value       = datahub_remote_executor_pool.aws_eks.urn
}

output "pool_state" {
  description = "Provisioning state of the pool (READY after a successful apply)."
  value       = datahub_remote_executor_pool.aws_eks.state_status
}

output "ingestion_source_urn" {
  description = "URN of the ingestion source pinned to the tf-example-aws-eks pool."
  value       = "urn:li:dataHubIngestionSource:${datahub_ingestion_source.s3.source_id}"
}

output "bucket_name" {
  description = "S3 bucket holding the seeded customers.csv object."
  value       = aws_s3_bucket.data.bucket
}

output "secret_arn" {
  description = "Secrets Manager secret holding the file-mounted access key."
  value       = aws_secretsmanager_secret.s3_ingest_credentials.arn
}

output "eks_update_kubeconfig_command" {
  description = "Fetch kubeconfig credentials for the example cluster."
  value       = "aws eks update-kubeconfig --region ${var.region} --name ${module.eks.cluster_name}"
}

output "eks_console_url" {
  description = "AWS Console page for the cluster (the Resources tab shows workloads, pods and events)."
  value       = "https://${var.region}.console.aws.amazon.com/eks/home?region=${var.region}#/clusters/${module.eks.cluster_name}"
}

output "remote_executors_url" {
  description = "DataHub UI page listing Remote Executor pools and their attached workers."
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
    1. Fetch cluster credentials:
         eval "$(terraform output -raw eks_update_kubeconfig_command)"
    2. Check the worker pod is Running (first image pull is multi-GB, be patient):
         kubectl -n ${local.executor_namespace} get pods
    3. Confirm the Secrets Manager secret is file-mounted:
         kubectl -n ${local.executor_namespace} exec "$(kubectl -n ${local.executor_namespace} get pod -o name | head -1)" -- ls /mnt/secrets
    4. Confirm the worker attached to the pool:
         ${local.datahub_web_url}/ingestion/remote-executors?hideSystem=true&page=1
    5. Trigger the ingestion source and watch customers.csv become a dataset.
       UI (Run on "TF Example AWS - S3 CSV"):
         ${local.datahub_web_url}/ingestion/sources?hideSystem=true&page=1
       or API:
         eval "$(terraform output -raw run_ingestion_command)"
    6. Browse the cluster in the AWS Console (workloads, pods, events):
         https://${var.region}.console.aws.amazon.com/eks/home?region=${var.region}#/clusters/${module.eks.cluster_name}
    Remember: AWS resources bill until you run terraform destroy.
  EOT
}
