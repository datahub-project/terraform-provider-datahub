resource "datahub_secret" "bq_creds" {
  name             = "bq-service-account-json"
  description      = "Service account for BigQuery ingestion"
  value            = file("${path.module}/bq-key.json")
  value_wo_version = 1
}

resource "datahub_ingestion_source" "bq" {
  source_name   = "BigQuery (prod)"
  cron_interval = "0 6 * * *"
  timezone      = "UTC"

  # Use $${SECRET_NAME} (double $) so HCL does not interpolate the braces --
  # DataHub resolves ${bq-service-account-json} at ingestion run time via Secrets.
  recipe = jsonencode({
    source = {
      type = "bigquery"
      config = {
        credential = {
          credentials_json = "$${bq-service-account-json}"
        }
      }
    }
    pipeline_name = "bigquery:prod"
  })

  depends_on = [datahub_secret.bq_creds]
}
