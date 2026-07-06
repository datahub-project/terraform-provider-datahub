data "datahub_assertion_assignment_rules" "all" {}

output "assignment_rule_urns" {
  description = "URNs of all DataHub Cloud assertion assignment rules."
  value       = data.datahub_assertion_assignment_rules.all.urns
}
