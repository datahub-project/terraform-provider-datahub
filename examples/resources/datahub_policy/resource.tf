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

# Metadata policy: scope privileges to an explicit list of resource URNs.
# This is the legacy resource-scope form, deprecated by DataHub. It still works,
# but prefer the filter form below for anything more expressive.
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

resource "datahub_tag" "snowflake_source" {
  tag_id = "source:snowflake"
  name   = "source:snowflake"
}

# Metadata policy: scope by criteria filter. Criteria combine with AND, and the
# values within one criterion combine with OR -- so this matches datasets and
# containers that also carry the source:snowflake tag. The legacy type/resources
# attributes cannot express either the tag predicate or the two entity types.
#
# Referencing datahub_tag.snowflake_source.urn rather than hardcoding the URN
# string puts the tag in Terraform's dependency graph, so it is created first.
resource "datahub_policy" "snowflake_owners_edit_tags" {
  policy_id   = "snowflake-owner-tag-editors"
  name        = "Snowflake Owners Can Edit Tags"
  type        = "METADATA"
  description = "Lets owners of Snowflake-sourced assets manage their tags"
  privileges  = ["EDIT_ENTITY_TAGS"]

  actors = {
    resource_owners = true
  }

  resources = {
    filter = {
      criteria = [
        {
          field  = "TYPE"
          values = ["dataset", "container"]
        },
        {
          field  = "TAG"
          values = [datahub_tag.snowflake_source.urn]
        },
      ]
    }
  }
}

# STARTS_WITH matches a URN prefix, which is the idiomatic way to scope a policy
# to a whole tag namespace without enumerating every tag in it.
resource "datahub_policy" "all_source_tagged_viewers" {
  policy_id  = "source-tagged-viewers"
  name       = "Source-Tagged Asset Viewers"
  type       = "METADATA"
  privileges = ["VIEW_ENTITY_PAGE"]

  actors = {
    groups = [datahub_corp_group.data_platform.urn]
  }

  resources = {
    filter = {
      criteria = [
        {
          field     = "TAG"
          values    = ["urn:li:tag:source:"]
          condition = "STARTS_WITH"
        },
      ]
    }
  }
}
