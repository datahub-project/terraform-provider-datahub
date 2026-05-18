data "datahub_me" "current" {}

output "current_urn" {
  value = data.datahub_me.current.urn
}
