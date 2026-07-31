# Pins a dataset's shape, so an unannounced column addition, removal or retype
# surfaces as a failure rather than breaking a downstream consumer.
#
# fields is the complete expected schema: with compatibility = "EXACT_MATCH"
# anything not listed here is a failure.
resource "datahub_schema_assertion" "orders_shape" {
  entity_urn    = "urn:li:dataset:(urn:li:dataPlatform:snowflake,analytics.orders,PROD)"
  compatibility = "EXACT_MATCH"
  description   = "Orders schema is stable; fails if columns are added, removed, or retyped."

  fields = [
    { path = "order_id", type = "STRING" },
    { path = "customer_id", type = "STRING" },
    { path = "order_total", type = "NUMBER" },
    { path = "created_at", type = "TIME" },
  ]

  evaluation_cron     = "0 */6 * * *"
  evaluation_timezone = "UTC"
  mode                = "ACTIVE"

  on_failure_actions = ["RAISE_INCIDENT"]
  on_success_actions = ["RESOLVE_INCIDENT"]
}
