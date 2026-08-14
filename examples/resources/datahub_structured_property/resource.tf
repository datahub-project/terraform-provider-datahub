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

# Setting an optional version is how you make a breaking change to a property
# that is already in use, and how you re-create a property_id whose earlier
# incarnation had values assigned to it. Deleting an assigned property does not
# release the Elasticsearch field DataHub derived from its qualified name, so
# re-creating the same property_id un-versioned is rejected as a field collision
# until an operator reindexes. A versioned definition derives a different field
# name, so it does not collide.
#
# The value must be exactly 14 digits, a yyyyMMddHHmmss timestamp; short forms
# such as "v1" are rejected by DataHub. Each new value has to be greater than
# the last, and changing it updates the property in place.
resource "datahub_structured_property" "classification" {
  property_id  = "tf-example-classification"
  value_type   = "string"
  entity_types = ["dataset"]
  version      = "20240610120000"

  display_name = "TF Example - Classification"
  description  = "Data sensitivity classification. Managed by Terraform."
}
