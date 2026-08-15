# Validates a metadata test definition against the DataHub Cloud test engine
# during `terraform plan`, without persisting anything. An invalid definition
# fails the plan with the server's validation messages instead of failing the
# apply.
data "datahub_metadata_test_validation" "check" {
  definition_json = jsonencode({
    on = {
      types = ["dataset"]
    }
    rules = {
      property = "glossaryTerms.terms.urn"
      operator = "exists"
    }
  })
}

# Thread the validated definition into the resource so the validation is
# ordered ahead of the write.
resource "datahub_metadata_test" "datasets_have_terms" {
  test_id         = "tf-example-datasets-have-terms"
  name            = "TF Example - Datasets Have Glossary Terms"
  category        = "Governance"
  definition_json = data.datahub_metadata_test_validation.check.definition_json
}
