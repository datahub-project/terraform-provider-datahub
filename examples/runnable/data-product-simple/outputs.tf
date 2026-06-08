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
