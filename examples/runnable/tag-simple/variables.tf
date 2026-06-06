variable "datahub_gms_url" {
  description = "DataHub GMS URL, used to build the verify_url output. Defaults to the DATAHUB_GMS_URL environment variable when empty."
  type        = string
  default     = ""
}
