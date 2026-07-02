terraform {
  required_version = ">= 1.11"

  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.12.0"
    }
  }
}

# Configure the DataHub provider.
# Credentials can also be supplied via DATAHUB_GMS_URL / DATAHUB_GMS_TOKEN
# environment variables.
provider "datahub" {}

# ---------------------------------------------------------------------------
# Tags
# ---------------------------------------------------------------------------

# A tag for data assets that contain personally identifiable information.
resource "datahub_tag" "pii" {
  tag_id      = "tf-example-pii"
  name        = "TF Example - PII"
  description = "Data asset contains personally identifiable information and requires special handling."
  color_hex   = "#E74C3C"
}

# A tag for data assets that have been verified and are safe for broad use.
resource "datahub_tag" "verified" {
  tag_id      = "tf-example-verified"
  name        = "TF Example - Verified"
  description = "Data asset has been reviewed and certified as production-ready."
  color_hex   = "#27AE60"
}

# A tag for data assets that are deprecated and should not be used for new work.
resource "datahub_tag" "deprecated" {
  tag_id      = "tf-example-deprecated"
  name        = "TF Example - Deprecated"
  description = "Data asset is deprecated. Migrate to a current alternative before the removal date."
  color_hex   = "#95A5A6"
}

# ---------------------------------------------------------------------------
# Look up a tag by ID (read-only reference, does not manage the resource).
# ---------------------------------------------------------------------------

data "datahub_tag" "pii_lookup" {
  tag_id = datahub_tag.pii.tag_id
}

# ---------------------------------------------------------------------------
# Enumerate all tags (useful as a for_each source for bulk import).
# ---------------------------------------------------------------------------

data "datahub_tags" "all" {
  depends_on = [
    datahub_tag.pii,
    datahub_tag.verified,
    datahub_tag.deprecated,
  ]
}
