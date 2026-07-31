# Runs arbitrary SQL against the dataset's platform and compares the result,
# for rules the typed assertions cannot express.
#
# statement must return a single value. sql_type = "METRIC" compares that value
# directly; "METRIC_CHANGE" compares it against the previous evaluation.
resource "datahub_sql_assertion" "orphaned_orders" {
  entity_urn = "urn:li:dataset:(urn:li:dataPlatform:snowflake,analytics.orders,PROD)"

  sql_type  = "METRIC"
  statement = "SELECT COUNT(*) FROM analytics.orders o LEFT JOIN analytics.customers c ON o.customer_id = c.id WHERE c.id IS NULL"
  operator  = "EQUAL_TO"
  value     = "0"

  description      = "No order may reference a customer that does not exist."
  failure_severity = "HIGH"

  evaluation_cron     = "0 */6 * * *"
  evaluation_timezone = "UTC"
  mode                = "ACTIVE"

  on_failure_actions = ["RAISE_INCIDENT"]
  on_success_actions = ["RESOLVE_INCIDENT"]
}
