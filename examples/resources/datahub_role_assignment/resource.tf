resource "datahub_corp_group" "data_platform" {
  group_id = "data-platform"
  name     = "Data Platform Team"
}

# Resolve the built-in Editor role to its URN.
data "datahub_role" "editor" {
  name = "Editor"
}

# Grant the Editor role to the group. DataHub allows one role per actor;
# define exactly one datahub_role_assignment per actor.
resource "datahub_role_assignment" "data_platform_editor" {
  actor_urn = datahub_corp_group.data_platform.urn
  role_urn  = data.datahub_role.editor.urn
}
