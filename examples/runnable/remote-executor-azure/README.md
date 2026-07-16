# Example: Remote Executor on Azure AKS (end-to-end)

**DataHub Cloud only.** Remote Executor pools, the executor worker image, and the Remote Executor UI do not exist in open-source DataHub.

> **COST WARNING.** Unlike the other runnable examples, this one creates billable Azure infrastructure: an AKS cluster with two `Standard_D4s_v5` nodes (roughly USD 280/month), plus a Key Vault and a storage account (a few dollars/month). Billing starts at `terraform apply` and stops only when you run `terraform destroy`. Do not leave it running unattended.

This example stands up a complete, working Remote Executor deployment from nothing, in a single `terraform apply`:

1. A DataHub **Remote Executor pool** named `azure-aks` (deliberately *not* the default pool).
2. An **AKS cluster** (2 nodes) with the secrets-store CSI driver + Azure Key Vault provider addon.
3. An **Azure Key Vault** holding the storage account key, file-mounted into the worker pods.
4. A **storage account** with a seeded `customers.csv` blob for the ingestion source to ingest.
5. The **Remote Executor worker** deployed onto AKS via the `datahub-executor-worker` Helm chart, attached to the `azure-aks` pool.
6. An **ingestion source** (Azure Blob Storage / `abs` type) pinned to the pool, whose recipe exercises **both** secret-resolution paths:
   - `${TF_EXAMPLE_ABS_ACCOUNT_NAME}` - a DataHub-managed secret, resolved via GMS
   - `${ABS_ACCOUNT_KEY}` - an Azure Key Vault secret, file-mounted at `/mnt/secrets/ABS_ACCOUNT_KEY` by the CSI driver

Both secrets are load-bearing: if either fails to resolve, the ingestion run visibly fails.

## Architecture notes

**Image path.** The Remote Executor image is pulled directly from DataHub's registry (`docker.datahub.com`, Cloudsmith-hosted) using a Kubernetes image pull secret built from your entitlement token. An ACR pull-through cache was evaluated and is not currently possible: Azure Container Registry's artifact-cache feature supports only a fixed list of upstream registries (Docker Hub, GHCR, Quay, ECR Public, and others), and arbitrary hosts are rejected. If you want the image served from your own ACR, run a one-time server-side copy instead: `az acr import --name <acr> --source docker.datahub.com/<repo>:<tag> --username re --password <token>`.

**Secret hygiene.** No secret value is stored in Terraform state: the DataHub secret uses the provider's write-only `value` attribute, the Key Vault secret uses `value_wo`, and both Kubernetes secrets (executor token and pull secret) use `data_wo`. Rotating any of them means bumping the corresponding `*_version`/`*_revision` counter. The flip side of write-only: Terraform cannot detect that a stored value differs from your variable, so if you applied with a wrong token, a plain `terraform apply` plans no change - force the rewrite with `terraform apply -replace=<resource>` (e.g. `-replace=kubernetes_secret_v1.cloudsmith_pull`).

**Single apply.** The kubernetes and helm providers are configured from the AKS cluster's kubeconfig, so Terraform defers their planning until the cluster exists within the same apply. Credentials are parsed from `kube_config_raw` rather than azurerm's structured `kube_config` attributes, which can come back with empty client certificates (see the comment in `providers.tf`). If the cluster is ever deleted out-of-band, remove the kubernetes/helm resources from state (`terraform state rm`) before destroying.

## Prerequisites

- A DataHub Cloud instance and a personal access token with the *Manage Metadata Ingestion* privilege (for the provider).
- A second access token of type **Remote Executor** (DataHub UI: **Settings > Access Tokens > Generate new token > Remote Executor**) - this is what the workers use to authenticate to GMS.
- A **Cloudsmith entitlement token** for `docker.datahub.com`, from your DataHub Cloud representative. The image lives at `docker.datahub.com/re/datahub-executor`; confirm the current image tag with them (`executor_image_tag`).
- An Azure subscription and `az login` completed (Terraform's azurerm provider uses the Azure CLI credential by default).
- `kubectl` and `helm` (optional - for verification and for the cleanup fallback).

Export credentials:

```bash
export DATAHUB_GMS_URL="https://your-instance.acryl.io"
export DATAHUB_GMS_TOKEN="<provider PAT>"
export TF_VAR_datahub_gms_url="$DATAHUB_GMS_URL/gms"
read -s TF_VAR_datahub_executor_token && export TF_VAR_datahub_executor_token
read -s TF_VAR_cloudsmith_token && export TF_VAR_cloudsmith_token
```

Optionally copy `terraform.tfvars.example` to `terraform.tfvars` for non-secret overrides (region, VM size, image path).

## Apply

```bash
terraform init
terraform apply
```

Expect roughly 10-15 minutes: AKS takes 5-10 minutes, the executor pool blocks until READY (30-90 seconds), and the Helm release waits for the worker to start - the first image pull is multi-gigabyte, so the pod can sit in `ContainerCreating` for several minutes, then `Running` but unready for a few more while the executor bootstraps and connects to GMS.

If the apply looks stuck on the executor Helm release, resist the urge to interrupt it hard: the release has a 10-minute timeout that fails cleanly and writes state. A single Ctrl-C is a graceful stop; a second Ctrl-C aborts immediately and can truncate the local state file mid-write, after which the next apply starts from scratch and collides with the resources that already exist. (If that happens: restore `terraform.tfstate` from `terraform.tfstate.backup`.)

## Verify

Follow the `next_steps` output:

```bash
terraform output next_steps
```

In short:

1. `eval "$(terraform output -raw aks_get_credentials_command)"` then `kubectl -n datahub-executor get pods` - the worker pod should be `Running`.
2. `kubectl -n datahub-executor exec <pod> -- ls /mnt/secrets` - shows `ABS_ACCOUNT_KEY`.
3. Open `terraform output -raw remote_executors_url` in a browser - the `azure-aks` pool shows one attached worker. (`terraform output -raw aks_portal_url` gives the Azure Portal view of the cluster: workloads, pods, live logs.)
4. Trigger a test ingest of *TF Example Azure Blob CSV (azure-aks pool)*: click **Run** on the page at `terraform output -raw ingestion_sources_url`, or trigger it from the terminal with `eval "$(terraform output -raw run_ingestion_command)"`. In testing the run completed within a minute or two; the first run can take longer if the worker needs to install the `abs` plugin into a fresh venv. On success, `customers.csv` appears as a dataset on the `abs` platform.

## Cleanup

```bash
terraform destroy
```

Dependency ordering tears the worker Helm release down before the pool, and the Key Vault is fully purged (purge protection is disabled) so the example can be re-applied immediately. If a destroy fails partway, re-run it; if the worker release lingers, delete it first (`helm -n datahub-executor uninstall datahub-executor`) and re-run the destroy.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Pod `ImagePullBackOff` | Wrong `executor_image_repository` path or an invalid/expired Cloudsmith token. Check `kubectl -n datahub-executor describe pod <pod>` (a `401 Unauthorized` from the registry login endpoint means the credential). Test the token directly: `curl -sS -o /dev/null -w '%{http_code}\n' -u "re:$TF_VAR_cloudsmith_token" "https://docker.datahub.com/v2/re/datahub-executor/tags/list"` - expect `200`. After correcting the token, run `terraform apply -replace=kubernetes_secret_v1.cloudsmith_pull` (write-only values are not diffed) and delete the pod to skip the backoff wait. |
| `Error: Unauthorized` on kubernetes/helm resources | The providers reached the API server without credentials. This example parses `kube_config_raw` to avoid the known empty-certificate case (see `providers.tf`); if it recurs, check that the cluster has local accounts enabled and is not Entra-only (`az aks show ... --query disableLocalAccounts`). |
| Pod `Pending` | CPU request does not fit the node. Keep `executor_cpu_request = "3"` on `Standard_D4s_v5` nodes (4 vCPU nodes have ~3.8 allocatable). |
| Pod `CreateContainerError` / mount failure | The CSI addon identity cannot read the Key Vault. Check the `csi_addon` access policy applied and the SecretProviderClass values. |
| Worker running but not listed under Remote Executors | Wrong `datahub_gms_url` (must end in `/gms`) or an invalid Remote Executor token. Check the pod logs. |
| Ingestion run fails resolving `${...}` | For `TF_EXAMPLE_ABS_ACCOUNT_NAME`, confirm the DataHub secret exists; for `ABS_ACCOUNT_KEY`, confirm step 2 above shows the mounted file (names are case-sensitive). |
