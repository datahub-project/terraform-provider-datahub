# A metadata test is a declarative governance rule DataHub evaluates against
# catalog entities. The definition is the test's own rule document, passed
# through as JSON; jsonencode keeps it readable as HCL rather than an
# embedded string. The `on` block selects the entities in scope, and the
# `rules` block holds the conditions those entities must pass.
resource "datahub_metadata_test" "prod_datasets_have_owners" {
  test_id     = "tf-example-prod-dataset-owners"
  name        = "TF Example - PROD Datasets Have Owners"
  category    = "Governance"
  description = "Every PROD dataset must have at least one owner."

  definition_json = jsonencode({
    on = {
      types = ["dataset"]
      conditions = {
        property = "origin"
        operator = "equals"
        value    = "PROD"
      }
    }
    rules = {
      property = "ownership.owners.owner"
      operator = "exists"
    }
  })
}
