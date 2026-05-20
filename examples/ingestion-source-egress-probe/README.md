# Ingestion Source Egress IP Probe

## What this example does

Creates a single `datahub_ingestion_source` resource using the DataHub `file` connector pointed at `https://ifconfig.me`. The source is configured as a manual-trigger-only ingestion with no schedule.

The `terraform apply` demonstrates the full resource lifecycle against a live DataHub instance: create, read (state refresh), and (via `terraform destroy`) delete.

The source itself serves a practical diagnostic purpose: when triggered from the DataHub UI, the ingestion run will fail to parse the response (ifconfig.me returns plain text, not MCE/MCP metadata JSON), but the **failure log reveals the executor's outbound IP address**. This is useful when configuring source-system network allow-lists for DataHub Cloud's managed ingestion executor.

## Prerequisites

- `DATAHUB_GMS_URL` set to your DataHub instance URL (e.g. `https://your-instance.acryl.io`)
- `DATAHUB_GMS_TOKEN` set to a Personal Access Token with permission to manage ingestion sources
- Terraform >= 1.0
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

ingestion_source_id = "terraform-egress-ip-probe-<hash>"
next_steps          = <<EOT
  ...source URN, DataHub UI link, and copy-pasteable curl commands...
EOT
source_urn          = "urn:li:dataHubIngestionSource:terraform-egress-ip-probe-<hash>"
```

The `next_steps` output contains copy-pasteable curl commands to trigger an ingestion run and check the result, with the source URN already embedded.

## Trigger the ingestion run and check the result

Follow the steps printed in the `next_steps` output. In summary:

1. Run the step-1 curl command to trigger the run — it returns an execution request URN captured in `$EXEC_URN`.
2. Run the step-2 curl command to check the status and read the `report` field.
3. The run fails to parse the ifconfig.me response. The `report` field contains the executor's egress IP.

Alternatively, trigger and inspect the run from the DataHub UI: navigate to **Ingestion**, find **Terraform Egress IP Probe**, click **Execute**, then open the run log.

## Cleanup

```bash
terraform destroy
```

This removes the ingestion source from DataHub. Note that DataHub performs a soft delete (marks the entity `removed`) rather than a hard purge; the source will no longer appear in the UI but the URN may remain in the metadata store.

## Why this recipe?

The DataHub `file` connector accepts an HTTPS URL as its `filename` config. Pointing it at `https://ifconfig.me` means:

- No source-system credentials required
- No other Terraform-managed infrastructure needed (no S3 bucket, no local file path)
- Works identically on DataHub Cloud (managed executor) and self-hosted OSS DataHub
- The intentional parse failure is harmless and exposes the executor IP in the run log
