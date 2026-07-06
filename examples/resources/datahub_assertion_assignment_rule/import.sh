# Import by bare rule_id (provider constructs the URN).
terraform import datahub_assertion_assignment_rule.postgres_monitors tf-example-postgres-monitors

# Import by full URN.
terraform import datahub_assertion_assignment_rule.postgres_monitors urn:li:assertionAssignmentRule:tf-example-postgres-monitors
