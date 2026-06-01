# Create a native login user. The reset URL lets them set their own password.
resource "datahub_local_user_login" "bob" {
  username  = "bob"
  full_name = "Bob Jones"
  email     = "bob@example.com"
}

output "bob_reset_url" {
  value     = datahub_local_user_login.bob.password_reset_url
  sensitive = true
}

# Optionally enrich the user's catalog profile after login is created.
resource "datahub_corp_user" "bob" {
  username     = datahub_local_user_login.bob.username
  display_name = "Bob Jones"
  title        = "Analytics Engineer"
}
