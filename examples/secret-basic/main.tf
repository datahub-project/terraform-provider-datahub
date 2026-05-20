terraform {
  required_version = ">= 1.11"
  required_providers {
    datahub = {
      source = "registry.terraform.io/datahub-project/datahub"
    }
  }
}

provider "datahub" {
  # Credentials from environment:
  #   DATAHUB_GMS_URL   - e.g. https://your-instance.acryl.io
  #   DATAHUB_GMS_TOKEN - personal access token
}

# Creates an encrypted DataHub Secret. The plaintext value is never stored in
# Terraform state; DataHub encrypts it server-side before persisting.
#
# Pass the secret value via the environment:
#   TF_VAR_secret_value="..." terraform apply
resource "datahub_secret" "demo_token" {
  name             = "demo-api-token"
  description      = "API token for the demo ingestion source"
  value            = var.secret_value
  value_wo_version = 1 # increment this integer to rotate the secret
}

# An ingestion source that references the secret via ${demo-api-token}.
# DataHub resolves the placeholder at run time, before the ingestion executor
# runs the recipe, so the plaintext value never appears in DataHub's stored
# recipe configuration.
resource "datahub_ingestion_source" "demo" {
  source_name = "Demo Source (uses secret)"

  recipe = jsonencode({
    source = {
      type = "demo-data"
      config = {
        # Reference the secret by its name inside ${...}.
        # DataHub substitutes the decrypted value when executing the run.
        api_token = "${demo-api-token}"
      }
    }
    pipeline_name = "demo"
  })

  depends_on = [datahub_secret.demo_token]
}
