# Fails when a dataset's row count leaves the expected range, catching partial
# loads that a freshness check would pass.
#
# volume_type chooses absolute (ROW_COUNT_TOTAL) or relative
# (ROW_COUNT_CHANGE) measurement. Pair a single_value with a comparison
# operator, or use min_value and max_value for a range.
resource "datahub_volume_assertion" "orders_minimum" {
  entity_urn = "urn:li:dataset:(urn:li:dataPlatform:snowflake,analytics.orders,PROD)"

  volume_type  = "ROW_COUNT_TOTAL"
  operator     = "GREATER_THAN_OR_EQUAL_TO"
  single_value = "1000"
  description  = "Orders must hold at least 1000 records; fewer indicates an ingestion gap."

  source_type         = "INFORMATION_SCHEMA"
  evaluation_cron     = "0 */6 * * *"
  evaluation_timezone = "UTC"
  mode                = "ACTIVE"

  on_failure_actions = ["RAISE_INCIDENT"]
  on_success_actions = ["RESOLVE_INCIDENT"]
}
