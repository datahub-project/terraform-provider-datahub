# Look up an existing group by its group_id (the URN suffix).
data "datahub_corp_group" "data_platform" {
  group_id = "data-platform"
}

output "data_platform_group_urn" {
  value = data.datahub_corp_group.data_platform.urn
}
