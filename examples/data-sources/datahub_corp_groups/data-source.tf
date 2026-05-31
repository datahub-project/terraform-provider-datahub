data "datahub_corp_groups" "all" {}

output "group_urns" {
  value = data.datahub_corp_groups.all.urns
}
