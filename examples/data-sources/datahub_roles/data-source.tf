data "datahub_roles" "all" {}

output "role_urns" {
  value = data.datahub_roles.all.urns
}
