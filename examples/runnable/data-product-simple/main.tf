terraform {
  required_version = ">= 1.11"

  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.10.0"
    }
  }
}

# Configure the DataHub provider.
# Credentials can also be supplied via DATAHUB_GMS_URL / DATAHUB_GMS_TOKEN
# environment variables.
provider "datahub" {}

# ---------------------------------------------------------------------------
# Domain
# Data products are scoped to a domain. Create one here so the example is
# self-contained. In production, reference an existing domain:
#   domain = data.datahub_domain.my_domain.urn
# ---------------------------------------------------------------------------

resource "datahub_domain" "sales" {
  domain_id   = "tf-example-dp-sales"
  name        = "TF Example - Sales"
  description = "Sales and revenue data assets."
}

# ---------------------------------------------------------------------------
# Data products
# ---------------------------------------------------------------------------

# The canonical orders data product for the Sales domain.
resource "datahub_data_product" "orders" {
  data_product_id = "tf-example-orders"
  name            = "TF Example - Orders"
  description     = "Curated set of order and fulfilment data assets for the Sales domain."
  domain          = datahub_domain.sales.urn
  external_url    = "https://example.com/data-catalog/orders"
  custom_properties = {
    tier    = "gold"
    contact = "data-platform@example.com"
  }
}

# A second product -- customer 360 profile data.
resource "datahub_data_product" "customer_360" {
  data_product_id = "tf-example-customer-360"
  name            = "TF Example - Customer 360"
  description     = "Unified customer profile combining CRM, web, and purchase history."
  domain          = datahub_domain.sales.urn
}

# ---------------------------------------------------------------------------
# Read back the two products via the singular data source.
#
# datahub_data_product looks up each product by ID via the strongly-consistent
# OpenAPI endpoint, so the reads are safe immediately after apply.
# ---------------------------------------------------------------------------

data "datahub_data_product" "orders_details" {
  data_product_id = datahub_data_product.orders.data_product_id
}

data "datahub_data_product" "customer_360_details" {
  data_product_id = datahub_data_product.customer_360.data_product_id
}

# ---------------------------------------------------------------------------
# Enumerate all data products by URN.
#
# datahub_data_products is backed by searchAcrossEntities (eventually
# consistent). Newly created products may not appear until the search index
# catches up. Its primary use is supplying URNs to an import {} block -- see
# the README for the two-pass pattern.
# ---------------------------------------------------------------------------

data "datahub_data_products" "all" {
  depends_on = [
    datahub_data_product.orders,
    datahub_data_product.customer_360,
  ]
}
