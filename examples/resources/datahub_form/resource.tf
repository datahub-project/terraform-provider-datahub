# A form asks asset owners to fill in structured property values, either to
# collect metadata (COMPLETION) or to attest compliance (VERIFICATION).
# Prompts reference structured properties by URN; use the resource reference
# so Terraform creates the property first.
resource "datahub_structured_property" "data_classification" {
  property_id  = "tf-example-form-classification"
  value_type   = "string"
  entity_types = ["dataset"]

  display_name = "TF Example - Form Classification"
  description  = "Data sensitivity classification collected via a form."
  allowed_values = [
    { string_value = "Public" },
    { string_value = "Internal" },
    { string_value = "Confidential" },
  ]
}

resource "datahub_form" "pii_verification" {
  form_id     = "tf-example-pii-verification"
  name        = "TF Example - PII Verification"
  description = "Quarterly attestation that datasets carry a classification."
  type        = "VERIFICATION"

  prompts = [{
    # Prompt ids must be globally unique across all forms; completion records
    # are keyed by them, so changing an id resets collected responses.
    id                      = "tf-example-pii-verification-classification"
    title                   = "Classify this dataset"
    description             = "Pick the sensitivity level that applies."
    type                    = "STRUCTURED_PROPERTY"
    structured_property_urn = datahub_structured_property.data_classification.urn
    required                = true
  }]

  # Who fills the form in. Omit the block to assign it to the owners of each
  # matched asset (the default).
  actors = {
    owners = true
    groups = ["urn:li:corpGroup:governance-team"]
  }

  # Assign the form automatically to every Snowflake dataset. Assignment runs
  # asynchronously server-side; removing or narrowing the filter stops future
  # assignment but does not retract the form from entities already assigned.
  dynamic_assignment = {
    or_filters = [{
      and = [
        {
          field  = "_entityType"
          values = ["DATASET"]
        },
        {
          field  = "platform.keyword"
          values = ["urn:li:dataPlatform:snowflake"]
        },
      ]
    }]
  }
}

output "form_urn" {
  description = "URN of the form (urn:li:form:<form_id>)."
  value       = datahub_form.pii_verification.urn
}
