terraform {
  required_version = ">= 1.11"
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "~> 0.8"
    }
  }
}

provider "datahub" {}

# Ingestion source: pushes a DatasetProfile (row count) for the SQLite table to
# DataHub Cloud. datahub ingest reads the SQLite file from the local machine and
# sends only the metadata (row count, schema) - the database itself is never
# queried by DataHub Cloud.
resource "datahub_ingestion_source" "sqlite_profile" {
  source_id = "tf-example-sqlite-assertion"
  name      = "TF Example - SQLite Assertion Dataset"
  type      = "sqlalchemy"
  schedule = {
    interval = "0 * * * *"
    timezone = "UTC"
  }
  recipe = jsonencode({
    source = {
      type = "sqlalchemy"
      config = {
        sqlalchemy_uri   = "sqlite:///./fixtures/test.db"
        table_pattern    = { allow = ["tf_test_data"] }
        database_pattern = { allow = ["tf_assertion_test"] }
        profiling = {
          enabled = true
        }
      }
    }
    pipeline_name = "tf-example-sqlite-assertion"
    datahub_api = {
      server = var.gms_url
      token  = var.gms_token
    }
  })
}

# Volume assertion: passes when the table has >= 100 rows.
# DataHub evaluates this against the most recent DatasetProfile ingested above -
# no live database query happens at evaluation time.
resource "datahub_volume_assertion" "row_count_check" {
  entity_urn          = "urn:li:dataset:(urn:li:dataPlatform:sqlite,tf_assertion_test.tf_test_data,PROD)"
  volume_type         = "ROW_COUNT_TOTAL"
  operator            = "GREATER_THAN_OR_EQUAL_TO"
  single_value        = "100"
  evaluation_cron     = "0 */8 * * *"
  evaluation_timezone = "UTC"
  source_type         = "DATAHUB_DATASET_PROFILE"
  mode                = "ACTIVE"
  on_success_actions  = ["RESOLVE_INCIDENT"]
  on_failure_actions  = ["RAISE_INCIDENT"]
  depends_on          = [datahub_ingestion_source.sqlite_profile]
}
