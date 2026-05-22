resource "datahub_ingestion_source" "example" {
  source_name        = "CSV Enricher Demo"
  remote_executor_id = "default"
  cron_interval      = "0 6 * * *"
  timezone           = "UTC"

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
    pipeline_name = "csv-enricher:demo"
  })
}
