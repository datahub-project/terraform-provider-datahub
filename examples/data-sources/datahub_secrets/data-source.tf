data "datahub_secrets" "all" {}

output "secret_urns" {
  value = data.datahub_secrets.all.urns
}
