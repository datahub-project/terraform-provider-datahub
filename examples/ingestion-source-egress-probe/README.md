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
source_urn          = "urn:li:dataHubIngestionSource:terraform-egress-ip-probe-<hash>"
trigger_command     = "curl -sS -X POST \"$DATAHUB_GMS_URL/api/graphql\" ... | jq -r ..."
```

## Trigger the ingestion run (optional)

The run will fail to parse the response from ifconfig.me (plain text, not MCE/MCP JSON), but the failure log reveals the executor's egress IP.

### Via curl (GraphQL API)

The `trigger_command` output contains a ready-to-use curl command with the source URN already embedded. Copy it directly from the apply output:

```bash
# Copy the trigger_command output and run it:
EXEC_URN=$(terraform output -raw trigger_command | bash)
echo "Execution request: $EXEC_URN"
```

Or retrieve and run it in one step:

```bash
EXEC_URN=$(eval "$(terraform output -raw trigger_command)")
echo "Execution request: $EXEC_URN"
```

### Via the DataHub UI

1. Open the DataHub UI and navigate to **Ingestion**.
2. Find the source named **Terraform Egress IP Probe**.
3. Click **Execute** to trigger a manual run.

## Check the result

The run is short (a single HTTP fetch attempt) and will complete within seconds.

### Via curl

```bash
# Poll status -- expect RUNNING briefly, then FAILURE
curl -sS -X POST "$DATAHUB_GMS_URL/api/graphql" \
  -H "Authorization: Bearer $DATAHUB_GMS_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"query\":\"query { executionRequest(urn: \\\"$EXEC_URN\\\") { result { status startTimeMs durationMs report } } }\"}" \
  | jq '.data.executionRequest.result'
```

The `report` field in the response (and in the DataHub UI run log) contains the connector's failure message, which includes the executor's outbound IP address.

### Via the DataHub UI

Open the failed run's log in **Ingestion** -> **Terraform Egress IP Probe** -> run history. The egress IP appears in the fetch-failure line -- something like `Failed to fetch https://ifconfig.me: ... <IP>`.

Add that IP to any source-system firewall or network allow-list that DataHub's executor needs to reach.

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
