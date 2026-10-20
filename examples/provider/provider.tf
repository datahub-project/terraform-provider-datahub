terraform {
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.27.0"
    }
  }
}

# Provider credentials are read from environment variables by default:
#
#   DATAHUB_GMS_URL   - the GMS URL, for example
#                       Cloud:       https://your-instance.acryl.io/gms
#                       Self-hosted: http://localhost:8080
#   DATAHUB_GMS_TOKEN - a DataHub personal access token
#
# Or use Terraform input variables, exported via TF_VAR_*:
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
# One of the two must be supplied, in configuration or environment.
# The provider deliberately does not read the CLI's ~/.datahubenv:
# the target instance should be set by this configuration, not by
# whichever instance the machine's CLI was last pointed at.
#
# Provider-level defaults (optional) attach labels to every resource the
# provider manages, wherever the entity type supports them - see the
# "Provider-level defaults" guide. A managed-by = "terraform" custom
# property is written automatically to newly created entities unless
# disabled with auto_properties = [].
#
#   provider "datahub" {
#     defaults = {
#       custom_properties = { team = "data-platform" }
#       tags              = ["urn:li:tag:terraform-managed"]
#     }
#   }
provider "datahub" {}

data "datahub_me" "current" {}

output "current_urn" {
  description = "DataHub URN of the authenticated user."
  value       = data.datahub_me.current.urn
}
