# Two custom ownership roles. DataHub's built-in types work here too, as
# ownership type entities: urn:li:ownershipType:__system__technical_owner,
# __system__business_owner, __system__data_steward, __system__none.
resource "datahub_ownership_type" "data_steward" {
  type_id     = "data-steward"
  name        = "Data Steward"
  description = "Curates definitions and quality rules for this asset."
}

resource "datahub_ownership_type" "data_owner" {
  type_id     = "data-owner"
  name        = "Data Owner"
  description = "Accountable for the asset: access decisions, retention, sign-off."
}

resource "datahub_glossary_term" "revenue" {
  term_id     = "revenue"
  name        = "Revenue"
  description = "Total revenue recognised in the reporting period."
}

resource "datahub_corp_group" "data_platform" {
  group_id = "data-platform"
  name     = "Data Platform"
}

# One resource per owned entity, holding a set of (owner, ownership type) pairs.
#
# The resource owns only the pairs it declares: owners assigned in the DataHub
# UI survive apply and destroy untouched, which is what makes it safe to point
# at a glossary people already curate by hand.
resource "datahub_entity_ownership" "revenue" {
  entity_urn = datahub_glossary_term.revenue.urn

  owner = [
    # Two owners sharing ONE ownership type. Ordinary rather than exceptional --
    # a role is usually held by more than one party -- and nothing about
    # (entity, ownership type) is a unique key.
    {
      owner_urn          = datahub_corp_group.data_platform.urn
      ownership_type_urn = datahub_ownership_type.data_steward.urn
    },
    {
      owner_urn          = "urn:li:corpuser:alice@example.com"
      ownership_type_urn = datahub_ownership_type.data_steward.urn
    },

    # The same group again, under a SECOND ownership type. One owner may hold
    # several roles; each pair is its own entry and its own edge in DataHub.
    {
      owner_urn          = datahub_corp_group.data_platform.urn
      ownership_type_urn = datahub_ownership_type.data_owner.urn
    },
  ]
}
