# examples/

This directory contains two distinct types of content:

## Doc fragments

Consumed by `make generate` (tfplugindocs) to produce the `docs/` directory.
These are code snippets only -- no `terraform {}` block, not runnable standalone.

- `data-sources/<name>/data-source.tf` -- embedded in `docs/data-sources/<name>.md`
- `resources/<name>/resource.tf` -- embedded in `docs/resources/<name>.md`

## Runnable examples

Self-contained Terraform configurations you can `terraform apply` directly.
Each has its own README with prerequisites and instructions.

- `provider-install-verification/` -- smoke test: verifies the provider binary loads and credentials are valid
- `ingestion-source-csv-enricher/` -- creates a real ingestion source using the csv-enricher connector; ingests test metadata artifacts from a stable upstream URL
