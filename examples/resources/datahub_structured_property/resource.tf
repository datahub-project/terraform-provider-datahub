# A structured property is a typed, governed field that can be assigned to
# entities. This defines the property; datahub_structured_property_assignment
# attaches a value to a particular entity.
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
