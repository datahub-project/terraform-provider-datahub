# Example: Remote Executor Pool (Basic)

Creates a DataHub Remote Executor Pool and routes an ingestion source to it.

**DataHub Cloud only.** Remote Executor Pools are not available on OSS DataHub.

## What this example does

1. Creates a `datahub_remote_executor_pool` named `analytics-team`.
2. Creates a `datahub_ingestion_source` (CSV enricher) that runs on that pool.

After applying, deploy one or more `datahub-executor-worker` containers (or pods)
with `DATAHUB_EXECUTOR_POOL_ID=analytics-team`. Workers connect outbound to
DataHub Cloud and self-attach to the pool; no inbound firewall rules needed.

## Prerequisites

- A DataHub Cloud instance.
- A personal access token with the **Manage Ingestion** privilege.

Export the credentials:

```bash
export DATAHUB_GMS_URL=https://your-instance.acryl.io/gms
export DATAHUB_GMS_TOKEN=your-token
```

## Apply

```bash
terraform init
terraform apply
```

Terraform waits for the pool to finish provisioning (PROVISIONING_PENDING ->
READY) before completing the apply. This typically takes 30-90 seconds on
DataHub Cloud.

## Verify

```bash
# Print the pool ID and URN
terraform output pool_id
terraform output pool_urn

# Confirm the pool is READY
terraform output pool_state
```

You can also navigate to **Settings > Remote Executors** in the DataHub Cloud UI
to see the new pool and its current status.

## Deploy a worker against this pool

Use the Acryl `datahub-executor-worker` Helm chart. Provide the pool ID from
Terraform output as the `executorPoolId` value:

```bash
helm upgrade --install my-executor oci://public.ecr.aws/acryl/datahub-executor-worker \
  --set executorPoolId="$(terraform output -raw pool_id)" \
  --set datahubGmsUrl="$DATAHUB_GMS_URL" \
  --set datahubAccessToken="$DATAHUB_GMS_TOKEN"
```

The output `helm_values_snippet` prints the `executorPoolId` line ready to paste
into a `values.yaml` file:

```bash
terraform output -raw helm_values_snippet
```

## Cleanup

```bash
terraform destroy
```

Destroying the pool terminates the SQS channel asynchronously. Any workers
still connected will stop receiving messages. Destroy or scale down workers
before running `terraform destroy` to avoid noisy reconnect loops.

If this pool is the current default executor pool, set another pool as default
first (via `is_default = true` on another pool resource) before destroying.
