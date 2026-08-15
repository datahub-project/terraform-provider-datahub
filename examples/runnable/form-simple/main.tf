terraform {
  required_version = ">= 1.11"
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.22.0"
    }
  }
}

provider "datahub" {
  # Credentials from environment:
  #   DATAHUB_GMS_URL   - e.g. https://your-instance.acryl.io
  #   DATAHUB_GMS_TOKEN - personal access token (needs the MANAGE_FORMS
  #                       privilege, plus Manage Structured Properties and
  #                       Manage Users and Groups for the supporting resources)
}

# -------------------------------------------------------------------------
# Structured properties the forms collect
# -------------------------------------------------------------------------

# Form prompts collect structured property VALUES, so the properties must be
# defined first. Reference them via .urn (not a raw URN string) so Terraform
# orders creation automatically.
#
# The forms below only DEFINE these properties; assigning values happens when
# someone responds to a form in the DataHub UI.
resource "datahub_structured_property" "sensitivity" {
  property_id  = "tf-example-form-sensitivity"
  value_type   = "string"
  cardinality  = "SINGLE"
  entity_types = ["dataset"]

  display_name = "TF Example Form - Sensitivity"
  description  = "Data sensitivity level, collected via a metadata form."

  allowed_values = [
    { string_value = "Public", description = "Safe to share externally" },
    { string_value = "Internal", description = "Internal use only" },
    { string_value = "Restricted", description = "Access on a need-to-know basis" },
  ]

  settings = {
    show_in_search_filters = true
  }
}

resource "datahub_structured_property" "review_cycle_days" {
  property_id  = "tf-example-form-review-cycle-days"
  value_type   = "number"
  cardinality  = "SINGLE"
  entity_types = ["dataset"]

  display_name = "TF Example Form - Review Cycle Days"
  description  = "How often (in days) the dataset's metadata is reviewed."
}

# -------------------------------------------------------------------------
# Who responds
# -------------------------------------------------------------------------

# A stewardship group assigned the forms alongside each asset's owners.
# Referencing the resource's .urn keeps ordering automatic and validated.
resource "datahub_corp_group" "stewards" {
  group_id    = "tf-example-form-stewards"
  name        = "TF Example Form - Data Stewards"
  description = "Responds to metadata collection and attestation forms."
}

# -------------------------------------------------------------------------
# COMPLETION form: collect missing metadata
# -------------------------------------------------------------------------

# A COMPLETION form asks the assigned actors to fill in structured property
# values that are missing. Prompt ids are required and must be globally
# unique across all forms: completion records are keyed by them, so changing
# an id resets the collected responses.
resource "datahub_form" "dataset_metadata" {
  form_id     = "tf-example-form-metadata"
  name        = "TF Example Form - Dataset Metadata"
  description = "Collects sensitivity and review-cycle metadata for warehouse datasets."
  type        = "COMPLETION"

  prompts = [
    {
      id                      = "tf-example-form-metadata-sensitivity"
      title                   = "How sensitive is the data in this dataset?"
      description             = "Pick the level that applies to the most sensitive column."
      type                    = "STRUCTURED_PROPERTY"
      structured_property_urn = datahub_structured_property.sensitivity.urn
      required                = true
    },
    {
      id                      = "tf-example-form-metadata-review-cycle"
      title                   = "How often is this dataset's metadata reviewed?"
      type                    = "STRUCTURED_PROPERTY"
      structured_property_urn = datahub_structured_property.review_cycle_days.urn
      required                = false
    },
  ]

  # Asset owners plus the stewardship group. Omit `users` and `groups`
  # rather than setting them to [] -- an explicit empty list reads back as
  # null and produces a plan that never converges.
  actors = {
    owners = true
    groups = [datahub_corp_group.stewards.urn]
  }

  # Assign the form automatically to every PostgreSQL dataset. Assignment
  # runs asynchronously server-side after the filter is written; narrowing
  # or removing the filter stops future assignment but does not retract the
  # form from entities already carrying it.
  dynamic_assignment = {
    or_filters = [{
      and = [
        {
          field  = "_entityType"
          values = ["DATASET"]
        },
        {
          field  = "platform.keyword"
          values = ["urn:li:dataPlatform:postgres"]
        },
      ]
    }]
  }
}

# -------------------------------------------------------------------------
# VERIFICATION form: attest existing metadata
# -------------------------------------------------------------------------

# A VERIFICATION form asks the actors to attest that the asset's metadata is
# correct; responding marks the asset verified rather than merely filled in.
# Actors are omitted here, which defaults to the owners of each matched asset.
resource "datahub_form" "sensitivity_attestation" {
  form_id     = "tf-example-form-attestation"
  name        = "TF Example Form - Sensitivity Attestation"
  description = "Owners attest that the recorded sensitivity level is still accurate."
  type        = "VERIFICATION"

  prompts = [{
    id                      = "tf-example-form-attestation-sensitivity"
    title                   = "Confirm the sensitivity level of this dataset"
    description             = "Verify the recorded value, correcting it if it has drifted."
    type                    = "STRUCTURED_PROPERTY"
    structured_property_urn = datahub_structured_property.sensitivity.urn
    required                = true
  }]

  dynamic_assignment = {
    or_filters = [{
      and = [
        {
          field  = "_entityType"
          values = ["DATASET"]
        },
        {
          field  = "platform.keyword"
          values = ["urn:li:dataPlatform:mysql"]
        },
      ]
    }]
  }
}

# -------------------------------------------------------------------------
# Enumerate forms
# -------------------------------------------------------------------------

# All form URNs visible to the authenticated principal. Backed by OpenSearch,
# so forms created within the last few seconds may not yet appear.
data "datahub_forms" "all" {
  depends_on = [
    datahub_form.dataset_metadata,
    datahub_form.sensitivity_attestation,
  ]
}
