data "datahub_policies" "all" {}

output "policy_urns" {
  value = data.datahub_policies.all.urns
}

# Before feeding these URNs into import {} blocks, narrow them to the policies
# this configuration is meant to own. The list includes the default policies
# DataHub ships, several of which grant through DataHub roles -- a binding the
# policy write API cannot carry, so importing and applying one would leave it
# granting nothing to anybody. The provider refuses that write, but the cleanest
# outcome is not to import them at all.
#
# Select by a naming convention of your own: an allowlist stays correct when a
# DataHub upgrade adds a new default policy, whereas a list of URNs to skip does
# not.
locals {
  managed_policy_urns = [
    for urn in data.datahub_policies.all.urns :
    urn if startswith(urn, "urn:li:dataHubPolicy:acme-")
  ]
}

output "managed_policy_urns" {
  value = local.managed_policy_urns
}
