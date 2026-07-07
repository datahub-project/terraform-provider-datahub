# A data contract bundles existing assertions on a dataset into an SLA.
# It references assertions authored by the typed assertion resources -- it does
# not create them -- and groups them into freshness, schema, and data-quality
# guarantees. Reference the assertions by `.urn` so Terraform creates them first.

# A custom (externally-evaluated) assertion works on both OSS and Cloud.
resource "datahub_custom_assertion" "orders_row_count" {
  entity_urn     = "urn:li:dataset:(urn:li:dataPlatform:postgres,tf_example.public.orders,PROD)"
  assertion_type = "Data Contract Check"
  description    = "TF Example - orders row count is non-zero"
  platform_urn   = "urn:li:dataPlatform:great-expectations"
}

resource "datahub_data_contract" "orders" {
  dataset_urn = "urn:li:dataset:(urn:li:dataPlatform:postgres,tf_example.public.orders,PROD)"
  state       = "ACTIVE"

  # Volume/field/SQL/custom checks go under data_quality; freshness assertions
  # under freshness_assertion_urns; schema assertions under schema_assertion_urns.
  data_quality_assertion_urns = [datahub_custom_assertion.orders_row_count.urn]
}

output "contract_urn" {
  description = "URN of the data contract (one per dataset)."
  value       = datahub_data_contract.orders.urn
}
