# Manage a DataHub user's catalog profile.
# For native-auth login credentials, see datahub_local_user_login.
resource "datahub_corp_user" "alice" {
  username     = "alice"
  display_name = "Alice Smith"
  full_name    = "Alice Jane Smith"
  email        = "alice@example.com"
  title        = "Data Engineer"
}

output "alice_urn" {
  value = datahub_corp_user.alice.urn
}
