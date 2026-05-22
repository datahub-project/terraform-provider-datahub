terraform {
  required_providers {
    datahub = {
      source = "registry.terraform.io/datahub-project/datahub"
    }
  }
}

provider "datahub" {
  # host is intentionally omitted here; set it via DATAHUB_HOST environment variable or ~/.datahubenv.
  # gms_token is intentionally omitted here; set it via the DATAHUB_GMS_TOKEN env var or ~/.datahubenv.
}

variable "remote_executor_id" {
  type        = string
  description = "DataHub remote executor ID. Use \"default\" for OSS DataHub and DataHub Cloud."
  default     = "default"
}

locals {
  # csv-enricher reads a delimited file and patches DataHub entity metadata.
  # No credentials required - the URL below points to a stable public test
  # fixture in the DataHub OSS repository.
  csv_enricher_recipe = jsonencode({
    source = {
      type = "csv-enricher"
      config = {
        filename        = "https://raw.githubusercontent.com/datahub-project/datahub/e32ee8df08404fa29f8b1630c9a7a6cf1ba270a2/metadata-ingestion/tests/integration/csv-enricher/csv_enricher_test_data.csv"
        array_delimiter = "|"
        delimiter       = ","
        write_semantics = "PATCH"
      }
    }
    pipeline_name = "csv-enricher:demo"
  })
}

# Scheduled ingestion source - runs daily at 06:00 UTC.
resource "datahub_ingestion_source" "csv_enricher_scheduled" {
  source_name        = "CSV Enricher (scheduled)"
  remote_executor_id = var.remote_executor_id
  cron_interval      = "0 6 * * *"
  timezone           = "UTC"
  cli_version        = "1.3.1.5" # omit for latest
  recipe             = local.csv_enricher_recipe
}

# On-demand ingestion source - no schedule, triggered manually or via API.
resource "datahub_ingestion_source" "csv_enricher" {
  source_name        = "CSV Enricher (on-demand)"
  remote_executor_id = var.remote_executor_id

  extra_args = {
    # NOTE: Avoid jsonencode() here: it HTML-escapes '<' into '<'.
    # DataHub expects a string containing a JSON array.
    extra_pip_requirements = "[\"acryl-datahub[csv-enricher]\"]"
  }

  recipe = local.csv_enricher_recipe
}
