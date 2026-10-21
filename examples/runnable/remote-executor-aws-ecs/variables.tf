variable "datahub_gms_url" {
  description = "DataHub Cloud GMS URL for the Remote Executor, e.g. https://your-instance.acryl.io/gms. Must end in /gms: DataHub Cloud routes GMS under /gms on the instance hostname, and the executor talks to GMS itself rather than to the UI proxy the provider uses at the bare host. Export TF_VAR_datahub_gms_url=\"$DATAHUB_GMS_URL/gms\" or set it in terraform.tfvars."
  type        = string

  validation {
    condition     = endswith(var.datahub_gms_url, "/gms")
    error_message = "datahub_gms_url must end in /gms (the Remote Executor requires the full GMS path)."
  }
}

variable "datahub_executor_token" {
  description = "DataHub personal access token of type Remote Executor, used by the executor to authenticate to GMS. Generate in the DataHub UI: Settings > Access Tokens > Generate new token > Remote Executor. This is distinct from DATAHUB_GMS_TOKEN. Pass via TF_VAR_datahub_executor_token."
  type        = string
  sensitive   = true
}

variable "executor_image_tag" {
  description = "Remote Executor image tag, passed to the template's ImageTag parameter. The image itself is fixed by the template to DataHub's ECR registry. This default duplicates the template's own default at the pinned template_git_ref; bump the two together."
  type        = string
  default     = "v2.3.1-cloud"
}

variable "template_git_ref" {
  description = "Git commit of github.com/acryldata/datahub-cloudformation to fetch remote-executor/datahub-executor.ecs.template.yaml from. A full commit SHA, not a branch: the repository has no release tags and master moves roughly weekly. Changing this is how a template update is taken."
  type        = string
  default     = "101bc80d5b565f99657603fca4fbe3290b58b449"
}

variable "executor_desired_count" {
  description = "Number of Remote Executor tasks the ECS service keeps running (the template's DesiredCount)."
  type        = number
  default     = 1
}

variable "executor_ephemeral_storage_gib" {
  description = "Ephemeral storage for the executor task in GiB (the template's TaskEphemeralStorageSizeInGiB; Fargate allows 21-200). The template's default of 21 sits a hair above the 20 GiB that evicted the executor on EKS once the multi-gigabyte image and the ingestion plugin venvs it installs on local disk had filled it. Fargate bills only the GiB above 20, roughly USD 2.40/month for the extra 30."
  type        = number
  default     = 50

  validation {
    condition     = var.executor_ephemeral_storage_gib >= 21 && var.executor_ephemeral_storage_gib <= 200
    error_message = "executor_ephemeral_storage_gib must be between 21 and 200 (the Fargate range)."
  }
}

variable "region" {
  description = "AWS region for all resources. Defaults to us-west-2 because that is where the executor image's ECR registry lives: every task start pulls a multi-gigabyte image through the NAT gateway, and a cross-region default would pay inter-region transfer on each one."
  type        = string
  default     = "us-west-2"
}

variable "default_tags" {
  description = "Tags applied to every AWS resource through the provider's default_tags. Resources the CloudFormation stack creates are tagged by CloudFormation with the stack's tags instead."
  type        = map(string)
  default = {
    project    = "tf-example-datahub-remote-executor"
    managed_by = "terraform"
  }
}
