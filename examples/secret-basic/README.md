# secret-basic

Creates a DataHub Secret and an ingestion source that references it via `${SECRET_NAME}` substitution.

## Prerequisites

- Terraform CLI 1.11 or later (required for WriteOnly attribute support)
- A running DataHub instance (OSS or Cloud)
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` set in the shell
- The token must belong to a principal with the `MANAGE_SECRETS` DataHub privilege

## How secret substitution works

1. `datahub_secret.demo_token` stores the plaintext under an encrypted form in DataHub. The value is never written to `terraform.tfstate`.
2. `datahub_ingestion_source.example` contains `"${TF_EXAMPLE_SECRET}"` in the recipe JSON.
3. When a run is triggered, the DataHub ingestion executor calls the `getSecretValues` GraphQL query, decrypts the named secrets, and substitutes the plaintext values into the recipe before passing it to `datahub ingest`. The cleartext value is only present in memory during execution.

## Apply

```bash
export DATAHUB_GMS_URL=https://your-instance.acryl.io
export DATAHUB_GMS_TOKEN=<personal-access-token>

# Set the secret value without it appearing in shell history
read -s TF_VAR_secret_value && export TF_VAR_secret_value

terraform init
terraform apply
```

## Rotating the secret

Because `value` is WriteOnly, Terraform has no record of the previous value and cannot detect drift automatically. To rotate:

1. Update the source of the value (e.g. generate a new API token).
2. Increment `value_wo_version` in `main.tf` (e.g. `1` -> `2`).
3. Run `terraform apply` -- Terraform plans a replacement (delete + create) of `datahub_secret.demo_token`.

The URN (`urn:li:dataHubSecret:TF_EXAMPLE_SECRET`) is unchanged after rotation because the name stays the same, so the recipe reference `${TF_EXAMPLE_SECRET}` continues to work without any recipe edit.

## Importing an existing secret

If the secret was created outside Terraform (e.g. via the DataHub UI), import it by name or URN:

```bash
terraform import datahub_secret.demo_token urn:li:dataHubSecret:TF_EXAMPLE_SECRET
# or equivalently:
terraform import datahub_secret.demo_token TF_EXAMPLE_SECRET
```

After import, run `terraform apply` with the `value` set in config. The update mutation requires the value on every call, so you must supply it before subsequent updates will succeed.

## Cleanup

`TF_VAR_secret_value` is not required for destroy.

```bash
terraform destroy
```
