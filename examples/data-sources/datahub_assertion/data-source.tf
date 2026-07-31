# Read a single assertion by URN. Assertion URNs are server-generated, so they
# come from datahub_assertions or from a managed resource's urn attribute rather
# than being written by hand.
data "datahub_assertion" "orders_freshness" {
  urn = "urn:li:assertion:1b2c3d4e-5f60-7182-93a4-b5c6d7e8f901"
}

output "orders_freshness_target" {
  description = "Dataset the assertion monitors, and the kind of check it performs."
  value = {
    entity_urn     = data.datahub_assertion.orders_freshness.entity_urn
    assertion_type = data.datahub_assertion.orders_freshness.assertion_type
  }
}
