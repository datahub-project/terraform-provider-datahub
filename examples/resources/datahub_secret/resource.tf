resource "datahub_secret" "bq_creds" {
  name             = "tf-bq-service-account-json"
  description      = "Service account for BigQuery ingestion"
  value            = file("${path.module}/bq-key.json")
  value_wo_version = 1 # bump to rotate
}
