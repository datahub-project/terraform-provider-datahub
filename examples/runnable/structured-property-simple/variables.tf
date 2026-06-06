variable "datahub_gms_url" {
  description = "DataHub GMS URL (e.g. https://your-datahub.example.com)."
  type        = string
}

variable "datahub_gms_token" {
  description = "DataHub personal access token."
  type        = string
  sensitive   = true
}
