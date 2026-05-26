variable "connection_id" {
  description = "Unique identifier for the connection. Becomes the URN suffix (urn:li:dataHubConnection:<connection_id>). Must be URL-safe."
  type        = string
  default     = "prod-snowflake"
}

variable "connection_name" {
  description = "Human-readable display name shown in the DataHub Integrations UI."
  type        = string
  default     = "Production Snowflake"
}

variable "snowflake_account_id" {
  description = "Snowflake account identifier (e.g. xy12345.us-east-1)."
  type        = string
}

variable "snowflake_username" {
  description = "Snowflake service user username."
  type        = string
}

variable "snowflake_warehouse" {
  description = "Snowflake warehouse name (e.g. COMPUTE_WH). Optional."
  type        = string
  default     = ""
}

variable "snowflake_role" {
  description = "Snowflake role to use. Leave empty to use the user's default role."
  type        = string
  default     = ""
}

variable "snowflake_password" {
  description = "Snowflake password. Pass via TF_VAR_snowflake_password to avoid shell history exposure."
  type        = string
  sensitive   = true
}
