terraform {
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.13.0"
    }
  }
}

provider "datahub" {
  # Credentials from environment:
  #   DATAHUB_GMS_URL   - e.g. https://your-instance.acryl.io
  #   DATAHUB_GMS_TOKEN - personal access token
}

data "datahub_me" "current" {}

output "authenticated_as" {
  value = data.datahub_me.current.urn
}
