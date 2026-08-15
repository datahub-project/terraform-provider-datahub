resource "datahub_glossary_node" "classification" {
  node_id = "classification"
  name    = "Data Classification"
}

resource "datahub_glossary_term" "sensitive_data" {
  term_id     = "sensitive-data"
  name        = "Sensitive Data"
  description = "Any data whose disclosure must be controlled."
  parent_node = datahub_glossary_node.classification.urn
}

resource "datahub_glossary_term" "pii" {
  term_id     = "pii"
  name        = "PII"
  description = "Personally identifiable information."
  parent_node = datahub_glossary_node.classification.urn
}

resource "datahub_glossary_term" "email_address" {
  term_id     = "email-address"
  name        = "Email Address"
  parent_node = datahub_glossary_node.classification.urn
}

# PII inherits from Sensitive Data: PII appears under Sensitive Data's
# "Inherits" section in the DataHub UI. One resource per edge; relationships
# added outside Terraform on the same terms are left untouched.
resource "datahub_glossary_term_relationship" "pii_is_sensitive" {
  term_urn          = datahub_glossary_term.pii.urn
  relationship_type = "isA"
  related_term_urn  = datahub_glossary_term.sensitive_data.urn
}

# PII contains Email Address ("Contains" in the UI).
resource "datahub_glossary_term_relationship" "pii_has_email" {
  term_urn          = datahub_glossary_term.pii.urn
  relationship_type = "hasA"
  related_term_urn  = datahub_glossary_term.email_address.urn
}
