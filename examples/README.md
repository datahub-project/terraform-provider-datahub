# examples/

This directory contains two distinct types of content.

## Doc fragments

Consumed by `make generate` (tfplugindocs) to produce the `docs/` directory.
These are code snippets only -- no `terraform {}` block, not runnable standalone.

- `provider/` -- provider configuration snippet embedded in `docs/index.md`
- `data-sources/<name>/data-source.tf` -- embedded in `docs/data-sources/<name>.md`
- `resources/<name>/resource.tf` -- embedded in `docs/resources/<name>.md`

## Runnable examples

Self-contained Terraform configurations you can `terraform apply` directly.
Each has its own README with prerequisites and instructions.

All runnable examples live under `runnable/`:

- `runnable/provider-install-verification/` -- smoke test: verifies the provider binary loads and credentials are valid
- `runnable/secret-basic/` -- manage a secret as a Terraform resource and reference it from an ingestion source recipe
- `runnable/ingestion-source-csv-enricher/` -- create an ingestion source using the csv-enricher connector
- `runnable/ingestion-source-lookup/` -- look up an existing ingestion source with a data source
- `runnable/connection-snowflake/` -- create a reusable Snowflake connection
- `runnable/connection-snowflake-ingestion-source/` -- create a Snowflake connection wired to an ingestion source
- `runnable/executor-pool-basic/` -- provision a remote executor pool and route an ingestion source to it
- `runnable/remote-executor-azure/` -- full Remote Executor deployment on Azure AKS: executor pool, worker via Helm, and an ingestion source using both DataHub-managed and Key Vault CSI-mounted secrets (creates billable Azure infrastructure)
- `runnable/local-iam/` -- set up local-auth users, group membership, a role assignment, and an access policy for a team
