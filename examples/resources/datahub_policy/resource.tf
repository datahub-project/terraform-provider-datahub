resource "datahub_corp_group" "data_platform" {
  group_id = "data-platform"
  name     = "Data Platform Team"
}

# Platform policy: grant administrative privileges to a group.
resource "datahub_policy" "platform_admins" {
  policy_id   = "data-platform-admins"
  name        = "Data Platform Admins"
  type        = "PLATFORM"
  description = "Lets the data platform team manage ingestion and secrets"
  privileges  = ["MANAGE_INGESTION", "MANAGE_SECRETS"]

  actors = {
    groups = [datahub_corp_group.data_platform.urn]
  }
}

# Metadata policy: scope privileges to specific resources.
resource "datahub_policy" "tag_editors" {
  policy_id  = "prod-dataset-tag-editors"
  name       = "Prod Dataset Tag Editors"
  type       = "METADATA"
  privileges = ["EDIT_ENTITY_TAGS"]

  actors = {
    groups = [datahub_corp_group.data_platform.urn]
  }

  resources = {
    type      = "dataset"
    resources = ["urn:li:dataset:(urn:li:dataPlatform:hive,sales.transactions,PROD)"]
  }
}
