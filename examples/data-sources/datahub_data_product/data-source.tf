# Look up a data product by its id, for example to reference its URN from a
# configuration that does not own it.
data "datahub_data_product" "orders" {
  data_product_id = "tf-example-orders"
}

output "orders_urn" {
  description = "URN of the Orders data product."
  value       = data.datahub_data_product.orders.urn
}
