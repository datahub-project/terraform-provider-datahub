output "assertion_urn" {
  description = "URN of the volume assertion in DataHub."
  value       = datahub_volume_assertion.row_count_check.urn
}

output "ingestion_source_urn" {
  description = "URN of the ingestion source used to profile the SQLite dataset."
  value       = "urn:li:dataHubIngestionSource:${datahub_ingestion_source.sqlite_profile.source_id}"
}

output "observe_url_hint" {
  description = "Navigation path to view the assertion result in DataHub."
  value       = "In DataHub: navigate to the tf_test_data dataset -> Observe tab -> run the assertion manually."
}

output "seed_command_150_rows" {
  description = "Command to seed 150 rows (assertion should PASS)."
  value       = "python3 fixtures/seed.py 150"
}

output "seed_command_50_rows" {
  description = "Command to seed 50 rows (assertion should FAIL)."
  value       = "python3 fixtures/seed.py 50"
}
