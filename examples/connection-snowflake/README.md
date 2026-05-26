# connection-snowflake

Creates a reusable, encrypted Snowflake credential in DataHub. After `terraform apply` the connection appears as a **Snowflake card** in **Settings > Integrations** in the DataHub Cloud UI, and can be referenced by ingestion source recipes so that credentials are managed centrally rather than inlined into each recipe.

## Prerequisites

- Terraform CLI 1.11 or later (WriteOnly attribute support required)
- DataHub Cloud instance (or OSS DataHub with a Snowflake-capable executor)
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` set in the shell
- The token must belong to a principal with the `MANAGE_CONNECTIONS` DataHub privilege
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

After apply, open **Settings > Integrations** in DataHub Cloud -- the Snowflake card should appear with your connection name.

## Using the connection in an ingestion source

Add the `connection` field to a recipe that targets the same Snowflake account:

```yaml
source:
  type: snowflake
  config:
    connection: <connection_urn output value>
```

Or in a `datahub_ingestion_source` resource:

```hcl
resource "datahub_ingestion_source" "snowflake" {
  source_name = "Snowflake Prod"
  recipe = jsonencode({
    source = {
      type = "snowflake"
      config = {
        connection = datahub_connection.snowflake.urn
      }
    }
  })
}
```

## Credential rotation

All fields inside the `snowflake` block are WriteOnly -- they are not stored in Terraform state. To rotate the password (or any other field):

1. Update the value in `terraform.tfvars` (or via `TF_VAR_snowflake_password`).
2. Increment `config_wo_version` in `main.tf` (e.g. `1` -> `2`).
3. Run `terraform apply` -- Terraform plans a destroy-before-create replacement.

The URN is unchanged after rotation, so any ingestion sources referencing it continue to work without modification.

## Importing an existing connection

If the connection was created in the DataHub UI, import it by URN:

```bash
terraform import datahub_connection.snowflake urn:li:dataHubConnection:prod-snowflake
```

After import, only `name` and `platform` are populated from DataHub (the config blob is encrypted and unavailable). Add the `snowflake` block with current credentials in your configuration and set `config_wo_version` before the next apply.

## Cleanup

```bash
terraform destroy
```
