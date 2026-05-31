# Resolve a built-in role (Admin, Editor, or Reader) to its URN.
data "datahub_role" "admin" {
  name = "Admin"
}

output "admin_role_urn" {
  value = data.datahub_role.admin.urn
}
