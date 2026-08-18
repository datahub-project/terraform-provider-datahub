terraform {
  required_version = ">= 1.11"
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.24.0"
    }
  }
}

provider "datahub" {
  # Credentials from environment:
  #   DATAHUB_GMS_URL   - e.g. https://your-instance.acryl.io/gms  (or http://localhost:8080 for OSS)
  #   DATAHUB_GMS_TOKEN - personal access token
}

# ---------------------------------------------------------------------------
# Metadata tests
#
# A metadata test is a declarative governance rule DataHub evaluates against
# catalog entities. The definition is the test's own rule document, passed
# through as JSON: the `on` block selects the entities in scope, and the
# `rules` block holds the conditions those entities must pass. jsonencode
# keeps it readable as HCL rather than an embedded string.
#
# The test entity and its create/update/delete API work on both OSS DataHub
# and DataHub Cloud, but only DataHub Cloud RUNS tests -- see the README for
# what that means when applying this example against OSS.
# ---------------------------------------------------------------------------

# Every dataset must carry a description -- either ingested from the source
# system (datasetProperties) or edited in the DataHub UI
# (editableDatasetProperties). The `or` combinator makes either one pass.
resource "datahub_metadata_test" "datasets_have_descriptions" {
  test_id     = "tf-example-mdtest-dataset-description"
  name        = "TF Example MDTest - Datasets Have Descriptions"
  category    = "Governance"
  description = "Every dataset must have a description, from the source system or edited in DataHub."

  definition_json = jsonencode({
    on = {
      types = ["dataset"]
    }
    rules = {
      or = [
        {
          property = "datasetProperties.description"
          operator = "exists"
        },
        {
          property = "editableDatasetProperties.description"
          operator = "exists"
        },
      ]
    }
  })
}

# Production datasets must have at least one owner. The `on.conditions`
# block narrows the scope: only entities matching it are evaluated, so
# non-production datasets are simply out of scope rather than failing.
resource "datahub_metadata_test" "prod_datasets_have_owners" {
  test_id     = "tf-example-mdtest-prod-owner"
  name        = "TF Example MDTest - PROD Datasets Have an Owner"
  category    = "Governance"
  description = "Every production dataset must have at least one owner assigned."

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

# ---------------------------------------------------------------------------
# Enumerate all metadata tests on the instance.
#
# Backed by the listTests GraphQL query, which is eventually consistent: a
# test created seconds ago may not appear until the search index catches up,
# so the tests above may be missing from the list on the first apply and
# present on the next plan. See outputs.tf for the bulk-import pattern this
# enables.
# ---------------------------------------------------------------------------

data "datahub_metadata_tests" "all" {}
