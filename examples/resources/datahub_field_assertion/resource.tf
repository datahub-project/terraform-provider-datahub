# Asserts something about one column rather than the dataset as a whole.
#
# field_assertion_type selects the mode: FIELD_METRIC evaluates an aggregate
# (metric) over the column, FIELD_VALUES evaluates every row against a
# threshold (fail_threshold_type and fail_threshold_value).
resource "datahub_field_assertion" "customer_id_not_null" {
  entity_urn = "urn:li:dataset:(urn:li:dataPlatform:snowflake,analytics.orders,PROD)"

  field_assertion_type = "FIELD_METRIC"
  field_path           = "customer_id"
  field_type           = "STRING"
  metric               = "NULL_COUNT"
  operator             = "EQUAL_TO"
  single_value         = "0"

  description      = "Every order must carry a customer_id."
  failure_severity = "HIGH"

  source_type         = "ALL_ROWS_QUERY"
  evaluation_cron     = "0 */6 * * *"
  evaluation_timezone = "UTC"
  mode                = "ACTIVE"

  on_failure_actions = ["RAISE_INCIDENT"]
  on_success_actions = ["RESOLVE_INCIDENT"]
}
