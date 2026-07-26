output "orders_urn" {
  description = "URN of the Orders data product."
  value       = datahub_data_product.orders.urn
}

output "customer_360_urn" {
  description = "URN of the Customer 360 data product."
  value       = datahub_data_product.customer_360.urn
}

output "orders_details" {
  description = "Orders data product details as read back by the singular data source."
  value = {
    data_product_id   = data.datahub_data_product.orders_details.data_product_id
    name              = data.datahub_data_product.orders_details.name
    description       = data.datahub_data_product.orders_details.description
    domain            = data.datahub_data_product.orders_details.domain
    external_url      = data.datahub_data_product.orders_details.external_url
    custom_properties = data.datahub_data_product.orders_details.custom_properties
  }
}

output "customer_360_details" {
  description = "Customer 360 data product details as read back by the singular data source."
  value = {
    data_product_id = data.datahub_data_product.customer_360_details.data_product_id
    name            = data.datahub_data_product.customer_360_details.name
    description     = data.datahub_data_product.customer_360_details.description
    domain          = data.datahub_data_product.customer_360_details.domain
  }
}

# All data product URNs visible to the authenticated principal (eventually
# consistent -- newly created products may not appear immediately).
output "all_data_product_urns" {
  description = "URNs of all DataHub data products returned by the search index."
  value       = data.datahub_data_products.all.urns
}

output "ui_url" {
  description = "DataHub UI path to verify the created data products."
  value       = "Navigate to Govern -> Data Products in the DataHub UI, or open $DATAHUB_GMS_URL/datahub/govern/dataProducts"
}

output "orders_tags_all" {
  description = "Orders' merged tags_all -- the marker tag, once var.enable_defaults = true. Null while defaults are off."
  value       = datahub_data_product.orders.tags_all
}

output "orders_structured_properties_defaults" {
  description = "Orders' structured_properties_defaults -- the marker property, once var.enable_defaults = true. Null while defaults are off."
  value       = datahub_data_product.orders.structured_properties_defaults
}

output "customer_360_tags_all" {
  description = "Customer 360's merged tags_all -- the marker tag, once var.enable_defaults = true. Null while defaults are off."
  value       = datahub_data_product.customer_360.tags_all
}

output "customer_360_structured_properties_defaults" {
  description = "Customer 360's structured_properties_defaults -- the marker property, once var.enable_defaults = true. Null while defaults are off."
  value       = datahub_data_product.customer_360.structured_properties_defaults
}

output "defaults_summary" {
  description = "What to do next, depending on whether provider defaults are on."
  value = var.enable_defaults ? (
    "Provider defaults are ON. Run: terraform output orders_tags_all / orders_structured_properties_defaults to see the merged values on both data products."
    ) : (
    "Provider defaults are OFF (enable_defaults = false). The bootstrap tag and structured property now exist. Re-apply with: terraform apply -var enable_defaults=true, to turn defaults on and see them appear on both data products above."
  )
}
