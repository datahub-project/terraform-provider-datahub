# Resolve a username to its URN for use as a policy actor, group member, or owner.
data "datahub_corp_user" "alice" {
  username = "alice"
}

output "alice_urn" {
  value = data.datahub_corp_user.alice.urn
}
