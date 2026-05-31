resource "datahub_corp_group" "data_platform" {
  group_id = "data-platform"
  name     = "Data Platform Team"
}

# Resolve an existing user to its URN (the provider does not create users).
data "datahub_corp_user" "alice" {
  username = "alice"
}

# Bind the user to the group. One resource per membership edge.
resource "datahub_corp_group_member" "alice_data_platform" {
  group_urn = datahub_corp_group.data_platform.urn
  user_urn  = data.datahub_corp_user.alice.urn
}
