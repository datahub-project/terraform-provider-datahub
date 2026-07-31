# Fails when a dataset has not been updated within the expected window, which
# is how a silently stalled upstream feed becomes visible.
#
# schedule_type selects which other attributes apply: FIXED_INTERVAL needs
# fixed_interval_unit and fixed_interval_multiple, CRON needs cron_schedule and
# cron_timezone. evaluation_cron is separate - it controls how often DataHub
# checks, not what it expects.
resource "datahub_freshness_assertion" "orders_daily" {
  entity_urn = "urn:li:dataset:(urn:li:dataPlatform:snowflake,analytics.orders,PROD)"

  schedule_type           = "FIXED_INTERVAL"
  fixed_interval_unit     = "HOUR"
  fixed_interval_multiple = 24
  description             = "Orders must refresh within 24 hours; a longer silence indicates a stalled feed."

  source_type         = "DATAHUB_OPERATION"
  evaluation_cron     = "0 */6 * * *"
  evaluation_timezone = "UTC"
  mode                = "ACTIVE"

  on_failure_actions = ["RAISE_INCIDENT"]
  on_success_actions = ["RESOLVE_INCIDENT"]
}
