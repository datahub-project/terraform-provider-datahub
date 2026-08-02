terraform {
  required_version = ">= 1.11"

  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.19.1"
    }
  }
}

locals {
  marker_tag_id       = "tf-example-dp-managed"
  marker_tag_urn      = "urn:li:tag:${local.marker_tag_id}"
  marker_property_id  = "io.example.terraform.dpManagedBy"
  marker_property_urn = "urn:li:structuredProperty:${local.marker_property_id}"
}

# Configure the DataHub provider.
# Credentials can also be supplied via DATAHUB_GMS_URL / DATAHUB_GMS_TOKEN
# environment variables.
#
# defaults.tags and defaults.structured_properties reference the marker tag
# and structured property below by their deterministic URN (not by a
# `datahub_tag.x.urn` / `datahub_structured_property.x.urn` reference) --
# provider configuration is evaluated once, before any resource in this apply
# runs, so it cannot depend on a resource this same apply would create. See
# variables.tf and the "Provider-level defaults" guide's bootstrap ordering
# section: apply once with the default var.enable_defaults = false to create
# the marker tag/property, then set it to true and re-apply.
provider "datahub" {
  defaults = var.enable_defaults ? {
    tags = [local.marker_tag_urn]
    structured_properties = {
      (local.marker_property_urn) = ["true"]
    }
  } : null
}

# ---------------------------------------------------------------------------
# Bootstrap: the marker tag and structured property that defaults.tags and
# defaults.structured_properties reference once enabled. These must exist
# before var.enable_defaults is set to true (see the provider block above).
# ---------------------------------------------------------------------------

resource "datahub_tag" "managed" {
  tag_id      = local.marker_tag_id
  name        = "TF Example DP - Managed"
  description = "Applied automatically to every data product this example's provider defaults manage."
}

resource "datahub_structured_property" "managed_by" {
  property_id  = local.marker_property_id
  display_name = "TF Example DP - Managed By Terraform"
  description  = "Search-filterable marker: value \"true\" means Terraform manages this data product."
  value_type   = "string"
  entity_types = ["dataProduct"]

  settings = {
    show_in_search_filters = true
  }
}

# ---------------------------------------------------------------------------
# Domain
# Data products are scoped to a domain. Create one here so the example is
# self-contained. In production, reference an existing domain:
#   domain = data.datahub_domain.my_domain.urn
# ---------------------------------------------------------------------------

resource "datahub_domain" "sales" {
  domain_id   = "tf-example-dp-sales"
  name        = "TF Example DP - Sales"
  description = "Sales and revenue data assets."
}

# ---------------------------------------------------------------------------
# Data products
# ---------------------------------------------------------------------------

# The canonical orders data product for the Sales domain.
resource "datahub_data_product" "orders" {
  data_product_id = "tf-example-dp-orders"
  name            = "TF Example DP - Orders"
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
  data_product_id = "tf-example-dp-customer-360"
  name            = "TF Example DP - Customer 360"
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
