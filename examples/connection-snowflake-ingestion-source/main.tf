terraform {
  required_version = ">= 1.11"
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.2.0"
    }
  }
}

provider "datahub" {
  # Credentials from environment:
  #   DATAHUB_GMS_URL   - e.g. https://your-instance.acryl.io
  #   DATAHUB_GMS_TOKEN - personal access token
}

# A reusable, encrypted Snowflake credential stored centrally in DataHub.
# Credentials are sent once on create (AES-GCM-256 encrypted server-side)
# and never returned by the read API. Increment config_wo_version to rotate.
#
# This connection serves two purposes:
#   1. Appears as a Snowflake card in Settings > Integrations in the DataHub UI.
#   2. Supplies credentials at ingestion runtime when referenced by the
#      connection: field in a Snowflake recipe -- the executor fetches and
#      decrypts the blob so credentials do not appear in the recipe itself.
resource "datahub_connection" "snowflake" {
  connection_id     = var.connection_id
  name              = var.connection_name
  config_wo_version = 1

  snowflake {
    account_id  = var.snowflake_account_id
    username    = var.snowflake_username
    warehouse   = var.snowflake_warehouse
    role        = var.snowflake_role
    auth_type   = "DEFAULT_AUTHENTICATOR"
    password_wo = var.snowflake_password
  }
}

# An ingestion source that references the connection above via its URN.
# The recipe omits credentials entirely -- the DataHub executor resolves the
# connection URN at runtime and injects the decrypted config fields.
#
# Note: connection: URN resolution is currently supported for Snowflake only.
# For other platforms, supply credentials directly in the recipe config.
resource "datahub_ingestion_source" "snowflake" {
  source_name = var.ingestion_source_name

  recipe = jsonencode({
    source = {
      type = "snowflake"
      config = {
        connection = datahub_connection.snowflake.urn

        database_pattern = {
          allow = var.database_allow_list
        }
      }
    }
  })
}
