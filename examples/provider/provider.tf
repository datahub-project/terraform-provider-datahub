terraform {
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "~> 0.1"
    }
  }
}

# Configuration via environment variables (recommended for CI and production):
#   DATAHUB_GMS_URL   - DataHub GMS URL, e.g. https://datahub.example.com
#   DATAHUB_GMS_TOKEN - DataHub personal access token
#
# Both attributes can also be set explicitly, or omitted entirely to fall back
# to the local DataHub CLI config (~/.datahubenv).
provider "datahub" {
  gms_url   = "https://datahub.example.com"
  gms_token = var.datahub_token
}
