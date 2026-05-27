data "datahub_connections" "all" {}

output "connection_urns" {
  value = data.datahub_connections.all.urns
}
