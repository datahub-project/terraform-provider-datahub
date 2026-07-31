# A data product groups related assets under one owned, documented bundle.
# domain expects a URN: reference another resource so Terraform orders the
# create correctly, rather than hardcoding the string.
resource "datahub_domain" "sales" {
  domain_id = "tf-example-sales"
  name      = "TF Example - Sales"
}

resource "datahub_data_product" "orders" {
  data_product_id = "tf-example-orders"
  name            = "TF Example - Orders"
  description     = "Curated set of order and fulfilment data assets for the Sales domain."
  domain          = datahub_domain.sales.urn
  external_url    = "https://example.com/data-catalog/orders"

  # Terraform owns this map completely: keys added elsewhere are removed on the
  # next apply.
  custom_properties = {
    tier    = "gold"
    contact = "data-platform@example.com"
  }
}
