output "urn" {
  description = "Full DataHub URN for the GC ingestion source."
  value       = data.datahub_ingestion_source.gc.urn
}

output "source_name" {
  description = "Human-readable display name of the source."
  value       = data.datahub_ingestion_source.gc.source_name
}

output "source_type" {
  description = "Ingestion connector type."
  value       = data.datahub_ingestion_source.gc.source_type
}

# recipe is returned as a JSON string. Use jsondecode() to extract fields.
output "recipe_connector_type" {
  description = "Connector type extracted from the recipe JSON using jsondecode."
  value       = jsondecode(data.datahub_ingestion_source.gc.recipe).source.type
}
