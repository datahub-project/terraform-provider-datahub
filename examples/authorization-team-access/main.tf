terraform {
  required_version = ">= 1.0"
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.3.0"
    }
  }
}

provider "datahub" {
  # Credentials from environment:
  #   DATAHUB_GMS_URL   - e.g. https://your-instance.acryl.io
  #   DATAHUB_GMS_TOKEN - personal access token (needs MANAGE_USERS_AND_GROUPS)
}

# A native DataHub group representing a team. group_id is the stable URN suffix
# (urn:li:corpGroup:<group_id>); name is the display name shown in the UI.
resource "datahub_corp_group" "data_platform" {
  group_id    = "data-platform"
  name        = "Data Platform Team"
  description = "Owns ingestion pipelines and platform configuration"
  email       = "data-platform@example.com"
  slack       = "#data-platform"
}

# Resolve the group back by id to demonstrate the lookup data source.
data "datahub_corp_group" "data_platform" {
  group_id = datahub_corp_group.data_platform.group_id
}
