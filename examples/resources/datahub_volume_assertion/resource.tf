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

# A new assertion has no history to judge against, so it stays uninformative
# until it has run for a while. backfill_start_date_ms seeds it from existing
# data instead -- here, 180 days before the assertion was written.
#
# The value is milliseconds since the Unix epoch, which is what the DataHub API
# takes. DataHub rejects anything more than 365 days back. Changing the value
# runs the backfill again; removing the attribute clears the request and leaves
# the history already gathered in place.
resource "datahub_volume_assertion" "orders_minimum_backfilled" {
  entity_urn = "urn:li:dataset:(urn:li:dataPlatform:snowflake,analytics.order_items,PROD)"

  volume_type  = "ROW_COUNT_TOTAL"
  operator     = "GREATER_THAN"
  single_value = "0"
  description  = "Order items must never be empty."

  source_type         = "INFORMATION_SCHEMA"
  evaluation_cron     = "0 */6 * * *"
  evaluation_timezone = "UTC"
  mode                = "ACTIVE"

  backfill_start_date_ms = 1769583799348 # 2026-01-28T05:43:19Z
}

# backfill_state reports whether the backfill is PENDING, COMPLETED, FAILED or
# REJECTED. A backfill produces no output of its own, so this is the only way to
# see that one was refused rather than run.
output "orders_backfill_state" {
  value = datahub_volume_assertion.orders_minimum_backfilled.backfill_state
}
