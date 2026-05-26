# Connection with Ingestion Source

This example shows a `datahub_connection` referenced by a `datahub_ingestion_source` recipe. The connection centralises credentials for a private-network Postgres database so that they are not inlined into the recipe blob.

## Prerequisites

- DataHub Cloud instance (or OSS DataHub with a running remote executor that supports connection resolution)
- Terraform 1.11+ (WriteOnly attribute support required)
- Postgres database reachable from the DataHub executor VPC

## Usage

```bash
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars with your values
terraform init
terraform apply
```

Verify the connection appears in the DataHub UI under **Settings > Integrations**, then trigger a manual ingestion run to confirm end-to-end wiring.

## Credential rotation

To rotate the Postgres password:

1. Update `postgres_password` in `terraform.tfvars`
2. Increment `config_wo_version` in `main.tf` (e.g., `1` -> `2`)
3. Run `terraform apply`

Terraform will destroy and recreate the connection with the new credentials. The ingestion source recipe continues to reference the same URN and picks up the rotated credentials on the next run.

## Cleanup

```bash
terraform destroy
```
