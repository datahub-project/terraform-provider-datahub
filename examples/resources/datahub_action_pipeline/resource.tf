# An action pipeline runs a DataHub-side automation on a schedule or trigger.
# The recipe is the pipeline's own configuration document, passed through as
# JSON; jsonencode keeps it readable as HCL rather than an embedded string.
#
# Note the doubled dollar in $${GCP_SA_KEY}: that escapes Terraform's own
# interpolation so DataHub receives the literal secret reference to resolve
# itself, keeping the credential out of Terraform state.
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
        project_id    = "my-gcp-project"
        credential    = "$${GCP_SA_KEY}"
        glossary_sync = { enabled = true }
      }
    }
  })
}
