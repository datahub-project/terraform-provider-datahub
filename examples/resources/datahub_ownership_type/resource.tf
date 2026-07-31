# A custom ownership type, selectable wherever DataHub asks who owns an asset.
# Supplying an explicit type_id keeps the URN stable across environments; the
# DataHub UI would assign a random UUID instead.
resource "datahub_ownership_type" "data_quality_lead" {
  type_id     = "tf-example-data-quality-lead"
  name        = "TF Example - Data Quality Lead"
  description = "Responsible for data quality monitoring, validation rules, and remediation."
}
