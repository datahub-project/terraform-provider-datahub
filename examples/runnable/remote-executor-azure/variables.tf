variable "datahub_gms_url" {
  description = "DataHub Cloud GMS URL for the Remote Executor workers, e.g. https://your-instance.acryl.io/gms. Must end in /gms. Export TF_VAR_datahub_gms_url=\"$DATAHUB_GMS_URL/gms\" or set it in terraform.tfvars."
  type        = string

  validation {
    condition     = endswith(var.datahub_gms_url, "/gms")
    error_message = "datahub_gms_url must end in /gms (the Remote Executor helm chart requires the full GMS path)."
  }
}

variable "datahub_executor_token" {
  description = "DataHub personal access token of type Remote Executor, used by the workers to authenticate to GMS. Generate in the DataHub UI: Settings > Access Tokens > Generate new token > Remote Executor. This is distinct from DATAHUB_GMS_TOKEN. Pass via TF_VAR_datahub_executor_token."
  type        = string
  sensitive   = true
}

variable "cloudsmith_token" {
  description = "Cloudsmith entitlement token used to pull the Remote Executor image from docker.datahub.com. Obtain from your DataHub Cloud representative. Pass via TF_VAR_cloudsmith_token."
  type        = string
  sensitive   = true
}

variable "cloudsmith_username" {
  description = "Username for the Remote Executor image registry."
  type        = string
  default     = "re"
}

variable "cloudsmith_registry" {
  description = "Login server of the Remote Executor image registry."
  type        = string
  default     = "docker.datahub.com"
}

variable "executor_image_repository" {
  description = "Repository path of the Remote Executor image under cloudsmith_registry."
  type        = string
  default     = "re/datahub-executor"
}

variable "executor_image_tag" {
  description = "Remote Executor image tag."
  type        = string
  default     = "v2.0.3-cloud"
}

variable "executor_chart_version" {
  description = "Version of the datahub-executor-worker helm chart (https://executor-helm.acryl.io)."
  type        = string
  default     = "0.0.52"
}

variable "executor_replica_count" {
  description = "Number of Remote Executor worker replicas."
  type        = number
  default     = 1
}

variable "executor_cpu_request" {
  description = "CPU request for each worker pod. The chart default is 4, which does not fit a Standard_D4s_v5 node (4 vCPU, ~3.8 allocatable after AKS system reservations) and would leave the pod permanently Pending. This is the sole deviation from the chart's default resource requests; memory stays at the chart default of 8Gi."
  type        = string
  default     = "3"
}

variable "location" {
  description = "Azure region for all resources."
  type        = string
  default     = "eastus2"
}

variable "resource_group_name" {
  description = "Name of the resource group that holds every Azure resource in this example."
  type        = string
  default     = "tf-example-datahub-remote-executor"
}

variable "node_vm_size" {
  description = "VM size for the AKS node pool."
  type        = string
  default     = "Standard_D4s_v5"
}

variable "node_count" {
  description = "Number of AKS worker nodes."
  type        = number
  default     = 2
}

variable "default_tags" {
  description = "Tags applied to every Azure resource. The azurerm provider has no provider-level default_tags, so these are applied per resource via local.tags."
  type        = map(string)
  default = {
    project    = "tf-example-datahub-remote-executor"
    managed_by = "terraform"
  }
}
