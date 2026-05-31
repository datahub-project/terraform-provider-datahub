terraform {
  required_version = ">= 1.11"
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
  group_id    = "tf-example-data-platform"
  name        = "TF Example - Data Platform Team"
  description = "Owns ingestion pipelines and platform configuration"
  email       = "data-platform@example.com"
  slack       = "#data-platform"
}

# Resolve the group back by id to demonstrate the lookup data source.
data "datahub_corp_group" "data_platform" {
  group_id = datahub_corp_group.data_platform.group_id
}

# Resolve an existing user by username. The provider does not create users, so
# member_username must already exist in DataHub (provisioned via SSO/JIT or the
# DataHub invite flow).
data "datahub_corp_user" "member" {
  username = var.member_username
}

# Add the user to the team group. One resource per membership edge.
resource "datahub_corp_group_member" "member" {
  group_urn = datahub_corp_group.data_platform.urn
  user_urn  = data.datahub_corp_user.member.urn
}

# Resolve the built-in Editor role to its URN.
data "datahub_role" "editor" {
  name = "Editor"
}

# Grant the Editor role to the team group. DataHub allows one role per actor,
# so define at most one role assignment per actor URN.
resource "datahub_role_assignment" "data_platform_editor" {
  actor_urn = datahub_corp_group.data_platform.urn
  role_urn  = data.datahub_role.editor.urn
}
