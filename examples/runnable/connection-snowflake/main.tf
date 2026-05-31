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
  #   DATAHUB_GMS_TOKEN - personal access token
}

# Creates a reusable, encrypted Snowflake credential in DataHub.
# After apply the connection appears in Settings > Integrations in the
# DataHub Cloud UI. Ingestion sources can reference it by URN so that
# credentials are managed centrally rather than inlined into each recipe.
#
# All fields inside the snowflake block are WriteOnly: they are sent to
# DataHub (which encrypts the config blob server-side with AES-GCM-256)
# but are never stored in Terraform state.
#
# Increment config_wo_version to rotate any credential -- Terraform will
# destroy and recreate the connection with the updated values.
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
