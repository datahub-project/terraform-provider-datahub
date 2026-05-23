terraform {
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

# See README.md. Creates a DataHub ingestion source using the csv-enricher
# connector pointed at a stable test CSV in the DataHub OSS repo. Triggering
# the source ingests real metadata artifacts that appear in DataHub search.
resource "datahub_ingestion_source" "csv_enricher" {
  source_name = "TF CSV Enricher"
  # "default" is the standard executor ID for OSS DataHub and DataHub Cloud.
  # If your instance uses a named custom executor, change this value.
  # A future datahub_ingestion_executor data source will allow lookup by name.
  remote_executor_id = "default"
  recipe = jsonencode({
    source = {
      type = "csv-enricher"
      config = {
        filename        = "https://raw.githubusercontent.com/datahub-project/datahub/e32ee8df08404fa29f8b1630c9a7a6cf1ba270a2/metadata-ingestion/tests/integration/csv-enricher/csv_enricher_test_data.csv"
        array_delimiter = "|"
        delimiter       = ","
        write_semantics = "PATCH"
      }
    }
    pipeline_name = "tf-csv-enricher"
  })
}

