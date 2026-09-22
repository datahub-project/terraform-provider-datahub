terraform {
  required_version = ">= 1.11"

  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.25.0"
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
  type_id     = "tf-example-ownership-data-quality-lead"
  name        = "TF Example Ownership - Data Quality Lead"
  description = "Responsible for data quality monitoring, validation rules, and remediation."
}

# A role for the upstream team that produces the data.
resource "datahub_ownership_type" "data_producer" {
  type_id     = "tf-example-ownership-data-producer"
  name        = "TF Example Ownership - Data Producer"
  description = "Upstream team or system that generates and publishes this data asset."
}

# ---------------------------------------------------------------------------
# Something to own, and someone to own it
#
# An ownership type on its own is an empty vocabulary entry. The rest of this
# example puts the two types above to work: a glossary term as the owned
# entity, a group as one owner principal, and a user as the other.
# ---------------------------------------------------------------------------

# The owned entity. A glossary term is a config entity this provider manages,
# so assigning owners to it is platform configuration rather than per-asset
# enrichment (which the provider deliberately leaves to the DataHub UI).
resource "datahub_glossary_term" "customer_ltv" {
  term_id     = "tf-example-ownership-customer-ltv"
  name        = "TF Example Ownership - Customer LTV"
  description = "Projected gross profit attributable to a customer over the whole relationship."
}

# One owner principal. A group needs no invite or sign-up flow, so it is safe
# to create on any instance, including a local Quickstart.
resource "datahub_corp_group" "analytics_stewards" {
  group_id    = "tf-example-ownership-analytics-stewards"
  name        = "TF Example Ownership - Analytics Stewards"
  description = "Stewards the analytics layer: metric definitions, quality rules, and consumer support."
}

# The second owner principal is supplied as a variable rather than created
# here. Creating a corp user would mean driving the sign-up endpoint, which
# refuses an address whose user entity already exists -- so one failed destroy
# would poison a fixed address permanently. The default is the built-in admin,
# which is present on every Quickstart and Cloud instance.

# ---------------------------------------------------------------------------
# Owner assignments
#
# One datahub_entity_ownership resource per owned entity, holding a set of
# (owner, ownership type) pairs. The resource owns only the pairs it declares:
# owners added in the DataHub UI survive apply and destroy untouched, which is
# what makes it safe to point at a glossary that people already curate by hand.
# ---------------------------------------------------------------------------

resource "datahub_entity_ownership" "customer_ltv" {
  entity_urn = datahub_glossary_term.customer_ltv.urn

  owner = [
    # Two owners sharing ONE ownership type. This is ordinary -- a role is
    # usually held by more than one party -- and nothing about
    # (entity, ownership type) is a unique key.
    {
      owner_urn          = datahub_corp_group.analytics_stewards.urn
      ownership_type_urn = datahub_ownership_type.data_quality_lead.urn
    },
    {
      owner_urn          = var.owner_user_urn
      ownership_type_urn = datahub_ownership_type.data_quality_lead.urn
    },

    # The same group again, under a SECOND ownership type. One owner may hold
    # several roles; each pair is its own element and its own edge in DataHub.
    {
      owner_urn          = datahub_corp_group.analytics_stewards.urn
      ownership_type_urn = datahub_ownership_type.data_producer.urn
    },
  ]
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
