output "connection_urn" {
  description = "URN of the created Postgres connection."
  value       = datahub_connection.prod_postgres.urn
}

output "ingestion_source_id" {
  description = "DataHub ingestion source ID."
  value       = datahub_ingestion_source.prod_postgres.source_id
}
