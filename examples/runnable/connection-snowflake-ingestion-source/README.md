# connection-snowflake-ingestion-source

Demonstrates how a `datahub_connection` is used as the credential back-end for a `datahub_ingestion_source`. After `terraform apply`:

- A Snowflake connection appears as a **Snowflake card** in **Settings > Integrations** in the DataHub Cloud UI.
- An ingestion source is created whose recipe contains `connection: <urn>` instead of inline credentials. When the executor runs the ingestion it fetches and decrypts the connection blob server-side -- credentials never appear in the recipe.

> **Snowflake only.** The `connection:` recipe field is currently resolved at ingestion runtime only for Snowflake sources. For other platforms (Databricks, BigQuery, Dataplex, Redshift) the connection is stored in DataHub and visible in Settings > Integrations, but credentials must still be supplied directly in the recipe config.

See `examples/runnable/connection-snowflake/` for a simpler example that creates the connection alone without an ingestion source.

## Prerequisites

- Terraform CLI 1.11 or later (WriteOnly attribute support required)
- DataHub Cloud instance (or OSS DataHub with a Snowflake-capable executor)
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` set in the shell
- The token must belong to a principal with `MANAGE_CONNECTIONS` and `MANAGE_INGESTION` DataHub privileges
- A Snowflake account with a service user, warehouse, and role ready for DataHub

## Apply

```bash
export DATAHUB_GMS_URL=https://your-instance.acryl.io
export DATAHUB_GMS_TOKEN=<personal-access-token>

cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars with your Snowflake details

# Pass the password via the environment to keep it out of files:
read -s TF_VAR_snowflake_password && export TF_VAR_snowflake_password

terraform init
terraform apply
```

After apply, open **Settings > Integrations** -- the Snowflake card should appear. Open **Ingestion** -- the ingestion source should be listed and can be run immediately.

## How credential resolution works

When the DataHub executor runs the ingestion source, it reads the recipe and sees:

```yaml
source:
  type: snowflake
  config:
    connection: urn:li:dataHubConnection:prod-snowflake
```

It calls `get_connection_json(urn)` to fetch the encrypted blob from DataHub, decrypts it, and merges the resulting fields (account_id, username, password, etc.) into the source config. The Snowflake source then connects using those resolved credentials.

## Credential rotation

All fields inside the `snowflake` block are WriteOnly -- they are not stored in Terraform state. To rotate:

1. Update `snowflake_password` in `terraform.tfvars` (or via `TF_VAR_snowflake_password`).
2. Increment `config_wo_version` in `main.tf` (e.g. `1` -> `2`).
3. Run `terraform apply` -- Terraform plans a destroy-before-create replacement of the connection.

The connection URN is unchanged after rotation, so the ingestion source recipe continues to reference the correct connection without modification.

## Cleanup

```bash
terraform destroy
```
