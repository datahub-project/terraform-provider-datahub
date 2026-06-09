variable "gms_url" {
  description = "DataHub GMS URL (used in the ingestion recipe for local CLI runs)."
  type        = string
  default     = ""
}

variable "gms_token" {
  description = "DataHub GMS token (used in the ingestion recipe for local CLI runs)."
  type        = string
  default     = ""
  sensitive   = true
}
