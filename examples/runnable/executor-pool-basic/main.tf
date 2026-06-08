terraform {
  required_version = ">= 1.11"
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.8.0"
    }
  }
}

provider "datahub" {
  # Credentials from environment:
  #   DATAHUB_GMS_URL   - your DataHub Cloud instance URL
  #   DATAHUB_GMS_TOKEN - personal access token with Manage Ingestion privilege
}

# Create a Remote Executor Pool. Workers deployed in your environment reference
# this pool by setting DATAHUB_EXECUTOR_POOL_ID=<pool_id> in their config.
resource "datahub_remote_executor_pool" "analytics" {
  pool_id     = "analytics-team"
  description = "Pool for analytics-team Remote Executor workers running in private VPC"
}

# Route an ingestion source to run on this pool.
resource "datahub_ingestion_source" "csv_enricher" {
  source_name        = "TF CSV Enricher (analytics pool)"
  remote_executor_id = datahub_remote_executor_pool.analytics.pool_id
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
    pipeline_name = "tf-csv-enricher-analytics"
  })
}
