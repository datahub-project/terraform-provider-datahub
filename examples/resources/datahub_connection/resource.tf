resource "datahub_connection" "prod_databricks" {
  connection_id     = "prod-databricks"
  name              = "Production Databricks"
  config_wo_version = 1

  databricks {
    workspace_url            = "https://dbc-39f83129-3f92.cloud.databricks.com"
    warehouse_id             = "397626f80afeea92"
    auth_type                = "PERSONAL_ACCESS_TOKEN"
    personal_access_token_wo = var.databricks_pat
  }
}

variable "databricks_pat" {
  description = "Databricks Personal Access Token"
  type        = string
  sensitive   = true
}
