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
  description = "CPU request for each worker pod. The chart default is 4, which does not fit an m6i.xlarge node (4 vCPU, roughly 3.9 allocatable after EKS system reservations, less the CSI driver and Pod Identity agent daemonsets) and would leave the pod permanently Pending. This is the sole deviation from the chart's default resource requests; memory stays at the chart default of 8Gi."
  type        = string
  default     = "3"
}

variable "csi_provider_chart_version" {
  description = "Version of the secrets-store-csi-driver-provider-aws helm chart (https://aws.github.io/secrets-store-csi-driver-provider-aws). Must be 0.3.11 or later for EKS Pod Identity support."
  type        = string
  default     = "3.1.4"
}

variable "region" {
  description = "AWS region for all resources."
  type        = string
  default     = "us-east-1"
}

variable "kubernetes_version" {
  description = "EKS Kubernetes version. Pinned explicitly because the version EKS picks when none is given is documented as not necessarily the latest, and a pin keeps two applies of this example alike."
  type        = string
  default     = "1.35"
}

variable "node_instance_type" {
  description = "Instance type for the EKS managed node group."
  type        = string
  default     = "m6i.xlarge"
}

variable "node_count" {
  description = "Number of EKS worker nodes."
  type        = number
  default     = 2
}

variable "node_disk_size" {
  description = "Root volume size in GiB for each EKS worker node. The executor image is multi-gigabyte and the executor installs ingestion plugins into venvs on local disk, so the AL2023 default of 20 GiB fills within minutes and the kubelet evicts the pod for ephemeral-storage pressure; 100 GiB of gp3 costs roughly USD 8/month per node."
  type        = number
  default     = 100
}

variable "default_tags" {
  description = "Tags applied to every AWS resource through the provider's default_tags."
  type        = map(string)
  default = {
    project    = "tf-example-datahub-remote-executor"
    managed_by = "terraform"
  }
}
