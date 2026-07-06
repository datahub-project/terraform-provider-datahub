# Auto-assign freshness and volume monitors to every Postgres dataset.
# One rule covers all matching datasets: new Postgres datasets are monitored
# automatically as they are ingested, with no per-dataset assertion authoring.
resource "datahub_assertion_assignment_rule" "postgres_monitors" {
  rule_id = "tf-example-postgres-monitors"
  name    = "TF Example - Postgres Freshness and Volume"

  # A dataset matches when it satisfies ANY or_filters group; within a group,
  # ALL facet predicates must match. This mirrors DataHub's search filter model.
  or_filters = [
    {
      and = [
        {
          field  = "platform"
          values = ["urn:li:dataPlatform:postgres"]
        }
      ]
    }
  ]

  # Freshness monitors: raise an incident on failure, resolve it on recovery.
  freshness = {
    source_type        = "INFORMATION_SCHEMA"
    on_failure_actions = ["RAISE_INCIDENT"]
    on_success_actions = ["RESOLVE_INCIDENT"]
  }

  # Volume monitors, using the same evaluation source.
  volume = {
    source_type = "INFORMATION_SCHEMA"
  }
}

output "rule_urn" {
  description = "URN of the assignment rule."
  value       = datahub_assertion_assignment_rule.postgres_monitors.urn
}
