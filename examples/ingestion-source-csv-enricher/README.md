# Ingestion Source CSV Enricher Example

## What this example does

Creates a single `datahub_ingestion_source` resource using the DataHub `csv-enricher` connector pointed at a commit-pinned test CSV file in the DataHub OSS repository. The source is configured as a manual-trigger-only ingestion with no schedule.

The `terraform apply` demonstrates the full resource lifecycle against a live DataHub instance: create, read (state refresh), and (via `terraform destroy`) delete.

When triggered, the ingestion run completes successfully and creates real metadata artifacts in DataHub -- the entities defined in the test CSV become searchable in the DataHub UI.

## Prerequisites

- `DATAHUB_GMS_URL` set to your DataHub instance URL (e.g. `https://your-instance.acryl.io`)
- `DATAHUB_GMS_TOKEN` set to a Personal Access Token with permission to manage ingestion sources
- Terraform >= 1.11
- `jq` for the optional curl commands below (or use the DataHub UI instead)

## Run

```bash
export DATAHUB_GMS_URL=https://your-instance.acryl.io
export DATAHUB_GMS_TOKEN=<your-token>

terraform init
terraform apply
```

Expected output after a successful apply:

```
Apply complete! Resources: 1 added, 0 changed, 0 destroyed.

Outputs:

ingestion_source_id = "tf-csv-enricher-<hash>"
next_steps          = <<EOT
  ...source URN, DataHub UI link, and copy-pasteable curl commands...
EOT
source_urn          = "urn:li:dataHubIngestionSource:tf-csv-enricher-<hash>"
```

The `next_steps` output contains copy-pasteable curl commands to trigger an ingestion run and check the result, with the source URN already embedded.

## Trigger the ingestion run and check the result

Follow the steps printed in the `next_steps` output. In summary:

1. Run the step-1 curl command to trigger the run -- it returns an execution request URN captured in `$EXEC_URN`.
2. Run the step-2 curl command to check the status and read the `report` field.
3. The run succeeds. The `report` field summarises the ingested entities. Navigate to the DataHub UI search to find them.

Alternatively, trigger and inspect the run from the DataHub UI: navigate to **Ingestion**, find **TF CSV Enricher**, click **Execute**, then open the run log.

## Cleanup

```bash
terraform destroy
```

This removes the ingestion source from DataHub. Note that DataHub performs a soft delete (marks the entity `removed`) rather than a hard purge; the source will no longer appear in the UI but the URN may remain in the metadata store. Metadata artifacts written by the ingestion run (entity descriptions, tags, etc.) are not removed by `terraform destroy` -- they remain in DataHub as PATCH-applied enrichments.

## Why this recipe?

The DataHub `csv-enricher` connector accepts an HTTPS URL as its `filename` config. Pointing it at a commit-pinned permalink in the DataHub OSS repo means:

- No source-system credentials required
- No other Terraform-managed infrastructure needed (no S3 bucket, no local file path)
- Works identically on DataHub Cloud (managed executor) and self-hosted OSS DataHub
- The URL is pinned to a specific commit SHA so it will not change or disappear
- The ingestion run completes without errors or warnings and creates real searchable metadata
