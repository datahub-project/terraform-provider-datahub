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

# datahub-gc is a built-in system source present on every DataHub instance.
# It was not created by Terraform; this example shows how to read any existing
# ingestion source by its ID -- whether it was created via the DataHub UI, the
# Python SDK, or a separate Terraform root module.
data "datahub_ingestion_source" "gc" {
  source_id = "datahub-gc"
}
