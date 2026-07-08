terraform {
  required_version = ">= 1.11"

  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.15.0"
    }
  }
}

# Configure the DataHub provider.
# Credentials can also be supplied via DATAHUB_GMS_URL / DATAHUB_GMS_TOKEN
# environment variables. Action pipelines are DataHub Cloud only.
provider "datahub" {}

# ---------------------------------------------------------------------------
# Action pipeline (automation)
# ---------------------------------------------------------------------------
#
# A Dataplex glossary-term sync: propagates DataHub glossary terms back to the
# Google Dataplex catalog. The recipe is a JSON document; secret values use
# `${SECRET_NAME}` placeholders (escaped as `$${...}` in HCL so Terraform does
# not interpolate them) which DataHub resolves from its Secrets at run time.
resource "datahub_action_pipeline" "dataplex_glossary_sync" {
  action_id   = "tf-example-dataplex-glossary-sync"
  name        = "TF Example - Dataplex Glossary Sync"
  type        = "dataplex_metadata_sync"
  category    = "Data Discovery"
  description = "Propagates DataHub glossary terms to the Dataplex catalog."
  executor_id = "default"

  recipe = jsonencode({
    action = {
      type = "dataplex_metadata_sync"
      config = {
        project_id       = "my-gcp-project"
        credential       = "$${GCP_SA_KEY}"
        glossary_sync    = { enabled = true }
        structured_props = { enabled = false }
      }
    }
  })
}

# ---------------------------------------------------------------------------
# Enumerate all action pipelines (useful as a for_each source for bulk import).
# ---------------------------------------------------------------------------

data "datahub_action_pipelines" "all" {
  depends_on = [datahub_action_pipeline.dataplex_glossary_sync]
}
