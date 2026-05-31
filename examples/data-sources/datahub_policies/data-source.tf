data "datahub_policies" "all" {}

output "policy_urns" {
  value = data.datahub_policies.all.urns
}
