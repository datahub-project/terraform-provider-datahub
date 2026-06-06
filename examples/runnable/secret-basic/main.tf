terraform {
  required_version = ">= 1.11"
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.7.0"
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
resource "datahub_secret" "example_secret" {
  name             = "TF_EXAMPLE_SECRET"
  description      = "Example secret value for the ingestion source recipe"
  value            = var.secret_value
  value_wo_version = 1 # increment this integer to rotate the secret
}

# An ingestion source that references the secret via ${TF_EXAMPLE_SECRET}.
# DataHub resolves the placeholder at run time, before the ingestion executor
# runs the recipe, so the plaintext value never appears in DataHub's stored
# recipe configuration.
resource "datahub_ingestion_source" "example" {
  source_name = "TF Example Source (uses secret)"

  recipe = jsonencode({
    source = {
      type = "demo-data"
      config = {
        # DataHub secret references use the syntax ${SECRET_NAME}. In HCL you
        # must write $${SECRET_NAME} (double $) so Terraform passes the literal
        # string "${SECRET_NAME}" through to DataHub rather than trying to
        # resolve it as an HCL variable. DataHub substitutes the decrypted
        # secret value at run time.
        api_token = "$${TF_EXAMPLE_SECRET}"
      }
    }
    pipeline_name = "tf-example"
  })

  depends_on = [datahub_secret.example_secret]
}
