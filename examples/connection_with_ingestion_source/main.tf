terraform {
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "~> 0.3"
    }
  }
}

provider "datahub" {}

# A reusable credential connection for a private-network Postgres database.
# The connection is referenced by the ingestion source recipe below, so that
# credentials are centrally managed and not inlined into the recipe blob.
resource "datahub_connection" "prod_postgres" {
  connection_id     = "prod-postgres"
  name              = "Production Postgres"
  config_wo_version = 1

  # raw_config is used here because DataHub does not yet have a first-party
  # Postgres connection type in OSS. Swap to a typed block once one is added.
  raw_config {
    platform_urn_suffix = "postgres"
    config_json_wo      = jsonencode({
      host_port = "${var.postgres_host}:5432"
      database  = var.postgres_db
      username  = var.postgres_user
      password  = var.postgres_password
    })
  }
}

# An ingestion source that references the connection above via its URN.
# The recipe uses the `connection` field to tell the DataHub executor which
# credential blob to inject at runtime.
resource "datahub_ingestion_source" "prod_postgres" {
  source_name = "prod-postgres-ingestion"
  recipe = jsonencode({
    source = {
      type = "postgres"
      config = {
        # The executor resolves this URN to the decrypted connection config
        # at run time, avoiding hardcoded credentials in the recipe blob.
        connection  = datahub_connection.prod_postgres.urn
        database    = var.postgres_db
        table_pattern = {
          allow = ["public.*"]
        }
      }
    }
  })

  depends_on = [datahub_connection.prod_postgres]
}
