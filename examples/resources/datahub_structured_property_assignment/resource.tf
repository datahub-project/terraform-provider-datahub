# Define a structured property that applies to domains.
resource "datahub_structured_property" "classification" {
  property_id  = "io.acme.classification"
  value_type   = "string"
  entity_types = ["domain"]
  allowed_values = [
    { string_value = "Public" },
    { string_value = "Confidential" },
  ]
}

resource "datahub_domain" "finance" {
  domain_id = "finance"
  name      = "Finance"
}

# Assign a value on the domain. Each resource is one (entity, property) edge;
# assignments merge per property, so multiple datahub_structured_property_assignment
# resources may target the same domain (one per property) without clobbering.
resource "datahub_structured_property_assignment" "finance_classification" {
  entity_urn              = datahub_domain.finance.urn
  structured_property_urn = datahub_structured_property.classification.urn
  values                  = ["Confidential"]
}
