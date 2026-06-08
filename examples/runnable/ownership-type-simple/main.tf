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

# ---------------------------------------------------------------------------
# Custom ownership types
# ---------------------------------------------------------------------------

# A role for the team responsible for data quality monitoring and remediation.
resource "datahub_ownership_type" "data_quality_lead" {
  type_id     = "tf-example-data-quality-lead"
  name        = "TF Example - Data Quality Lead"
  description = "Responsible for data quality monitoring, validation rules, and remediation."
}

# A role for the upstream team that produces the data.
resource "datahub_ownership_type" "data_producer" {
  type_id     = "tf-example-data-producer"
  name        = "TF Example - Data Producer"
  description = "Upstream team or system that generates and publishes this data asset."
}

# ---------------------------------------------------------------------------
# Enumerate all ownership types, then fetch full details for each one.
#
# datahub_ownership_types returns URNs via listOwnershipTypes (eventually
# consistent). Without depends_on, Terraform evaluates the list during the
# plan phase so the URNs are known and the for_each below can be keyed on
# them. The trade-off: newly created types do not appear in the details
# output until the next plan or refresh, once the GraphQL index has caught up.
# ---------------------------------------------------------------------------

data "datahub_ownership_types" "all" {}

data "datahub_ownership_type" "details" {
  for_each = toset(data.datahub_ownership_types.all.urns)
  type_id  = trimprefix(each.value, "urn:li:ownershipType:")
}
