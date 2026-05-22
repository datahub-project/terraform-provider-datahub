terraform {
  required_providers {
    datahub = {
      source = "registry.terraform.io/datahub-project/datahub"
    }
  }
}

provider "datahub" {
  # host is intentionally omitted here; set it via DATAHUB_HOST environment variable or ~/.datahubenv.
  # gms_token is intentionally omitted here; set it via the DATAHUB_GMS_TOKEN env var or ~/.datahubenv.
}

variable "databricks_workspace_url" {
  type        = string
  description = "Databricks workspace URL, e.g. https://my-workspace-databricks.net"
}

variable "databricks_pat" {
  type        = string
  description = "Databricks Personal Access Token used by the ingestion recipe"
  sensitive   = true
}

variable "databricks_warehouse_id" {
  type        = string
  description = "Databricks SQL Warehouse ID"
}

variable "remote_executor_id" {
  type        = string
  description = "Optional DataHub remote executor ID"
  default     = null
}

variable "aws_region" {
  type        = string
  description = "AWS region for DynamoDB ingestion (required by the connector)"
}

locals {
  datahub_domain = "my-datahub-domain"

  unity_recipe = jsonencode({
    source = {
      type = "unity-catalog"
      config = {
        workspace_url = var.databricks_workspace_url
        env           = "STG"
        token         = var.databricks_pat
        warehouse_id  = var.databricks_warehouse_id
        catalogs      = ["my_catalog"]

        schema_pattern = {
          allow = ["my_catalog.my_schema.*"]
          deny  = ["information_schema"]
        }

        table_pattern = {
          allow = ["my_catalog.my_schema.*"]
        }

        domain = {
          my_datahub_domain = {
            allow = [".*"]
          }
        }

        include_ownership      = true
        include_table_lineage  = true
        include_column_lineage = true
        lineage_data_source    = "API"

        profiling = {
          enabled       = true
          method        = "ge"
          max_wait_secs = 60
        }

        stateful_ingestion = {
          enabled             = true
          fail_safe_threshold = 85
        }
      }
    }
    pipeline_name = "unity-catalog:my-unity-source"
  })

  dynamodb_recipe = jsonencode({
    source = {
      type = "dynamodb"
      config = {
        # Credentials are optional; if omitted the connector can use AWS default credential discovery.
        # aws_access_key_id     = "$${AWS_ACCESS_KEY_ID}"
        # aws_secret_access_key = "$${AWS_SECRET_ACCESS_KEY}"
        # NOTE: Prefer DataHub Secrets / env vars instead of raw credentials.
        aws_region = var.aws_region
        env        = "STG"

        stateful_ingestion = {
          enabled             = true
          fail_safe_threshold = 85
        }
      }
    }
    pipeline_name = "dynamodb:my-dynamodb-source"
  })
}

resource "datahub_ingestion_source" "unity_scheduled" {
  # source_id   = "scheduled-my-unique-source-id" # automatically generated if not provided; derived from a sha256 version of source_name if not provided.
  source_name = "scheduled-my-unity-source-name"
  # source_type is optional; derived from recipe.source.type if omitted.
  remote_executor_id = "my-remote-executor-id"
  cron_interval      = "0 10 * * *"
  #  timezone           = "UTC" # as default timezone
  cli_version = "1.3.1.5" # omit for latest
  # async       = false # as default

  recipe = local.unity_recipe
}

resource "datahub_ingestion_source" "dynamodb_scheduled" {
  source_name = "scheduled-my-dynamodb-source-name"
  # source_type is optional; derived from recipe.source.type if omitted.
  remote_executor_id = "my-remote-executor-id"
  cron_interval      = "0 10 * * *"
  #  timezone           = "UTC" # as default timezone
  cli_version = "1.3.1.5" # omit for latest
  # async       = false # as default

  recipe = local.dynamodb_recipe
}

resource "datahub_ingestion_source" "unity" {
  # source_id   = "scheduled-my-unique-source-id" # automatically generated if not provided; derived from a sha256 version of source_name if not provided.
  source_name = "my-unity-source-name"
  # source_type is optional; derived from recipe.source.type if omitted.
  remote_executor_id = "my-remote-executor-id"

  extra_args = {
    # NOTE: Avoid jsonencode() here: it HTML-escapes '<' into '\u003c'.
    # DataHub expects a string containing a JSON array.
    extra_pip_requirements = "[\"setuptools<82.0.0\"]"
  }

  recipe = local.unity_recipe
  # async       = false # as default
}

resource "datahub_ingestion_source" "dynamodb" {
  source_name = "my-dynamodb-source-name"
  # source_type is optional; derived from recipe.source.type if omitted.
  remote_executor_id = "my-remote-executor-id"
  recipe             = local.dynamodb_recipe
  # async       = false # as default
}
