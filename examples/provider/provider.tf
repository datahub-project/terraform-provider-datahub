terraform {
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.14.0"
    }
  }
}

# Provider credentials are read from environment variables by default:
#
#   DATAHUB_GMS_URL   - DataHub GMS URL, e.g. https://your-instance.acryl.io/gms
#   DATAHUB_GMS_TOKEN - DataHub personal access token
#
# If you prefer Terraform input variables, export them via TF_VAR_* instead:
#
#   TF_VAR_datahub_gms_url   - passed as var.datahub_gms_url
#   TF_VAR_datahub_gms_token - passed as var.datahub_gms_token
#
# and configure the provider explicitly:
#
#   provider "datahub" {
#     gms_url   = var.datahub_gms_url
#     gms_token = var.datahub_gms_token
#   }
#
# Both attributes can also be omitted entirely to fall back to the local
# DataHub CLI config at ~/.datahubenv.
provider "datahub" {}

data "datahub_me" "current" {}

output "current_urn" {
  description = "DataHub URN of the authenticated user."
  value       = data.datahub_me.current.urn
}
