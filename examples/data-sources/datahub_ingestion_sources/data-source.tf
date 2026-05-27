data "datahub_ingestion_sources" "all" {}

output "ingestion_source_urns" {
  value = data.datahub_ingestion_sources.all.urns
}
