# Look up an existing ingestion source by its source_id.
# For UI-created sources the source_id appears in the browser URL:
#   https://<your-datahub>/ingestion/sources/<source_id>
data "datahub_ingestion_source" "warehouse" {
  source_id = "prod-postgres-warehouse-abc123"
}

# Export the URN for use by another Terraform root module or external tooling.
output "warehouse_urn" {
  value = data.datahub_ingestion_source.warehouse.urn
}

# Extract a field from the recipe. recipe is a JSON-encoded string;
# use jsondecode() to access individual fields.
output "warehouse_recipe_host" {
  value = jsondecode(data.datahub_ingestion_source.warehouse.recipe).source.config.host_port
}
