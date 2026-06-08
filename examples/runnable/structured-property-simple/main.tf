terraform {
  required_version = ">= 1.11"

  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.8.0"
    }
  }
}

# Configure the DataHub provider.
# Credentials can also be supplied via DATAHUB_GMS_URL / DATAHUB_GMS_TOKEN
# environment variables.
provider "datahub" {}

# A number-valued structured property that records the data retention period
# for datasets. Single-valued: each dataset has exactly one retention period.
resource "datahub_structured_property" "retention_days" {
  property_id  = "tf-example-retention-days"
  value_type   = "number"
  cardinality  = "SINGLE"
  entity_types = ["dataset"]

  display_name = "TF Example - Retention Days"
  description  = "Data retention period in days. Managed by Terraform."
  immutable    = false

  settings = {
    show_in_search_filters = true
    show_in_asset_summary  = true
  }
}

# A string-valued structured property for data classification, restricted to
# three allowed values, applicable to datasets and dashboards.
resource "datahub_structured_property" "classification" {
  property_id  = "tf-example-classification"
  value_type   = "string"
  cardinality  = "SINGLE"
  entity_types = ["dataset", "dashboard"]

  display_name = "TF Example - Data Classification"
  description  = "Data sensitivity classification. Managed by Terraform."

  allowed_values = [
    { string_value = "Public", description = "Data that can be shared externally" },
    { string_value = "Internal", description = "Internal use only - not for external sharing" },
    { string_value = "Confidential", description = "Sensitive data requiring restricted access" },
  ]

  settings = {
    show_in_search_filters = true
  }
}

# Read back the retention_days property via the singular data source.
data "datahub_structured_property" "retention_lookup" {
  property_id = datahub_structured_property.retention_days.property_id
}

# Enumerate all structured properties defined in DataHub.
# Backed by OpenSearch - newly created properties may take a few seconds to appear.
data "datahub_structured_properties" "all" {
  depends_on = [
    datahub_structured_property.retention_days,
    datahub_structured_property.classification,
  ]
}
