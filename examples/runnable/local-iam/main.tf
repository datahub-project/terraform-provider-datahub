terraform {
  required_version = ">= 1.11"
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.13.0"
    }
  }
}

provider "datahub" {
  # Credentials from environment:
  #   DATAHUB_GMS_URL   - e.g. https://your-instance.acryl.io
  #   DATAHUB_GMS_TOKEN - personal access token (needs MANAGE_USERS_AND_GROUPS
  #                       and MANAGE_USER_CREDENTIALS privileges)
}

# -------------------------------------------------------------------------
# Group
# -------------------------------------------------------------------------

# A native DataHub group representing a team. group_id is the stable URN
# suffix (urn:li:corpGroup:<group_id>); name is the display name in the UI.
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

# -------------------------------------------------------------------------
# Users
# -------------------------------------------------------------------------

# Create a human team member with native login credentials. The provider
# generates a random throwaway password and exposes a single-use 24h reset
# link so the user sets their own password -- Terraform never holds a real
# credential.
#
# Use an email address as the username: email-format usernames work on OSS
# (as a free-form identifier) and are required on Cloud (where the URN is
# derived from the email field).
resource "datahub_local_user_login" "team_member" {
  username  = var.new_member_email
  full_name = "TF Example - New Team Member"
  email     = var.new_member_email
}

# Create a catalog-only user entity representing a pipeline bot or service
# account. This mirrors what DataHub metadata ingestion produces when a
# source (e.g. dbt, BigQuery) discovers a user as a dataset owner: a corpUser
# entity with profile data but no login credentials. The entity appears in the
# DataHub Users UI as inactive -- expected, since it has no login credentials.
# datahub_corp_user writes the profile aspects only; no credentials are set.
resource "datahub_corp_user" "pipeline_bot" {
  username     = "tf-example-pipeline-bot"
  display_name = "TF Example - Pipeline Bot"
  email        = "tf-example-pipeline-bot@example.com"
  title        = "Automation"
}

# Resolve a pre-existing user by username (e.g. the Quickstart bootstrap admin
# or a user provisioned outside Terraform via SSO/JIT). This demonstrates the
# lookup pattern for users the provider did not create.
data "datahub_corp_user" "member" {
  username = var.member_username
}

# -------------------------------------------------------------------------
# Group membership
# -------------------------------------------------------------------------

# Add the newly created login user to the team group.
resource "datahub_corp_group_member" "team_member" {
  group_urn = datahub_corp_group.data_platform.urn
  user_urn  = datahub_local_user_login.team_member.user_urn
}

# Add the pipeline bot (catalog-only) to the team group so it can be used as
# a group-scoped owner on metadata assets.
resource "datahub_corp_group_member" "pipeline_bot" {
  group_urn = datahub_corp_group.data_platform.urn
  user_urn  = datahub_corp_user.pipeline_bot.urn
}

# Add the pre-existing user to the team group.
resource "datahub_corp_group_member" "member" {
  group_urn = datahub_corp_group.data_platform.urn
  user_urn  = data.datahub_corp_user.member.urn
}

# -------------------------------------------------------------------------
# Role and policy
# -------------------------------------------------------------------------

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

# Grant the team specific platform privileges via an access policy. Roles give
# broad presets; policies grant a precise privilege set to chosen actors.
resource "datahub_policy" "data_platform_admins" {
  policy_id   = "tf-example-data-platform-admins"
  name        = "TF Example - Data Platform Admins"
  type        = "PLATFORM"
  description = "Lets the data platform team manage ingestion and secrets"
  privileges  = ["MANAGE_INGESTION", "MANAGE_SECRETS"]

  actors = {
    groups = [datahub_corp_group.data_platform.urn]
  }
}
