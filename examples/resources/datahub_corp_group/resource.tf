resource "datahub_corp_group" "data_platform" {
  group_id    = "data-platform"
  name        = "Data Platform Team"
  description = "Owns ingestion pipelines and platform configuration"
  email       = "data-platform@example.com"
  slack       = "#data-platform"
}

# Use the group URN as an owner or policy actor elsewhere in your configuration.
output "data_platform_group_urn" {
  value = datahub_corp_group.data_platform.urn
}
