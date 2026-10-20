# Example: Remote Executor on AWS EKS (end-to-end)

**DataHub Cloud only.** Remote Executor pools, the executor worker image, and the Remote Executor UI do not exist in open-source DataHub.

> **COST WARNING.** Unlike most other runnable examples, this one creates billable AWS infrastructure: an EKS cluster (control plane roughly USD 73/month) with two `m6i.xlarge` nodes (roughly USD 280/month on demand), plus a Secrets Manager secret and an S3 bucket (well under a dollar a month). Call it USD 350/month. Billing starts at `terraform apply` and stops only when you run `terraform destroy`. Do not leave it running unattended.

This is the AWS counterpart of [`remote-executor-azure`](../remote-executor-azure/). The two directories are laid out file for file (`eks.tf` where the other has `aks.tf`, `secretsmanager.tf` for `keyvault.tf`, `s3.tf` for `storage.tf`), with the same variable names wherever the concept exists on both clouds, so `diff -r` across the two shows exactly what differs between clouds and nothing else.

It stands up a complete, working Remote Executor deployment from nothing, in a single `terraform apply`:

1. A DataHub **Remote Executor pool** named `tf-example-aws-eks` (deliberately *not* the default pool).
2. A **VPC and EKS cluster** (2 nodes) with the Secrets Store CSI driver, its AWS provider, and the EKS Pod Identity agent.
3. A read-only **IAM user** for the bucket, whose access key is held in **Secrets Manager** and file-mounted into the worker pods.
4. An **S3 bucket** with a seeded `customers.csv` object for the ingestion source to ingest.
5. The **Remote Executor worker** deployed onto EKS via the `datahub-executor-worker` Helm chart, attached to the `tf-example-aws-eks` pool.
6. An **ingestion source** (`s3` type) pinned to the pool, whose recipe exercises **both** secret-resolution paths:
   - `${TF_EXAMPLE_AWS_S3_BUCKET_NAME}` - a DataHub-managed secret, resolved via GMS
   - `${S3_ACCESS_KEY_ID}` and `${S3_SECRET_ACCESS_KEY}` - the two halves of one Secrets Manager secret, file-mounted at `/mnt/secrets/<name>` by the CSI driver

All three secrets are load-bearing: if any fails to resolve, the ingestion run visibly fails.

## Architecture notes

**Image path.** The Remote Executor image is pulled directly from DataHub's registry (`docker.datahub.com`, Cloudsmith-hosted) using a Kubernetes image pull secret built from your entitlement token. An ECR pull-through cache is not an option: like Azure's artifact cache, ECR supports only a fixed list of upstream registries (Docker Hub, ECR Public, GHCR, Quay, Kubernetes registry and a few others), and arbitrary hosts are rejected. If you want the image served from your own ECR, copy it once with `docker pull`, `docker tag` and `docker push` (or `crane copy`) and point `cloudsmith_registry` / `executor_image_repository` at it.

**Pod identity, not IRSA.** The CSI provider needs an AWS identity to read Secrets Manager on the worker pod's behalf. This example uses EKS Pod Identity (`aws_eks_pod_identity_association` on the executor's namespace and service account) rather than IAM Roles for Service Accounts, for two reasons: the executor chart does not create a service account by default and exposes annotations only when it does, and Pod Identity needs no OIDC provider on the cluster. The chart is told to create a service account under a fixed name (`serviceAccount.create = true`), and that name is what the association binds the role to.

**How the secret is split.** The access key is stored as one JSON document (`{"access_key_id": ..., "secret_access_key": ...}`). The `SecretProviderClass` uses the AWS provider's `jmesPath` parameter to mount each key as its own file, so the recipe can reference `${S3_ACCESS_KEY_ID}` and `${S3_SECRET_ACCESS_KEY}` separately. The whole document is also mounted, as `s3-ingest-credentials.json`.

**Secret hygiene.** No *copy* of a secret value is written to Terraform state beyond the resource that originates it: the DataHub secret uses the provider's write-only `value` attribute, the Secrets Manager version uses `secret_string_wo`, and both Kubernetes secrets (executor token and pull secret) use `data_wo`. The access key itself is a computed attribute of `aws_iam_access_key` and is in state, as is any credential Terraform creates; the write-only attributes stop it being copied a second, third and fourth time. Rotating any of them means bumping the corresponding `*_version`/`*_revision` counter. The flip side of write-only: Terraform cannot detect that a stored value differs from your variable, so if you applied with a wrong token, a plain `terraform apply` plans no change - force the rewrite with `terraform apply -replace=<resource>` (e.g. `-replace=kubernetes_secret_v1.cloudsmith_pull`).

**Single apply.** The kubernetes and helm providers are configured from the EKS module's outputs, so Terraform defers their planning until the cluster exists within the same apply. They authenticate by running `aws eks get-token` at apply time, so the `aws` CLI must be on `PATH` and see the same credentials Terraform uses. If the cluster is ever deleted out-of-band, remove the kubernetes/helm resources from state (`terraform state rm`) before destroying.

**Networking.** Two public subnets across two availability zones (EKS requires at least two), nodes with public IPs, no NAT gateway. The nodes need outbound access only - to the image registry, GMS, and PyPI for ingestion plugins - and a NAT gateway would add roughly USD 32/month for nothing the example demonstrates. This is an example shape, not a production one.

## Prerequisites

- A DataHub Cloud instance and a personal access token with the *Manage Metadata Ingestion* privilege (for the provider).
- A second access token of type **Remote Executor** (DataHub UI: **Settings > Access Tokens > Generate new token > Remote Executor**) - this is what the workers use to authenticate to GMS.
- A **Cloudsmith entitlement token** for `docker.datahub.com`, from your DataHub Cloud representative. The image lives at `docker.datahub.com/re/datahub-executor`; confirm the current image tag with them (`executor_image_tag`).
- An AWS account and credentials in the environment or a named profile (`AWS_PROFILE`), with permission to create VPC, EKS, IAM, Secrets Manager and S3 resources. The `aws` CLI must be installed: the kubernetes and helm providers run it to obtain cluster tokens.
- `kubectl` and `helm` (optional - for verification and for the cleanup fallback).

Export credentials:

```bash
export DATAHUB_GMS_URL="https://your-instance.acryl.io"
export DATAHUB_GMS_TOKEN="<provider PAT>"
export TF_VAR_datahub_gms_url="$DATAHUB_GMS_URL/gms"
read -s TF_VAR_datahub_executor_token && export TF_VAR_datahub_executor_token
read -s TF_VAR_cloudsmith_token && export TF_VAR_cloudsmith_token
```

Optionally copy `terraform.tfvars.example` to `terraform.tfvars` for non-secret overrides (region, instance type, image path).

## Apply

```bash
terraform init
terraform apply
```

Expect roughly 15-20 minutes: the EKS control plane takes 8-10 minutes and the node group a few more, the executor pool blocks until READY (30-90 seconds), and the Helm release waits for the worker to start - the first image pull is multi-gigabyte, so the pod can sit in `ContainerCreating` for several minutes, then `Running` but unready for a few more while the executor bootstraps and connects to GMS.

If the apply looks stuck on the executor Helm release, resist the urge to interrupt it hard: the release has a 10-minute timeout that fails cleanly and writes state. A single Ctrl-C is a graceful stop; a second Ctrl-C aborts immediately and can truncate the local state file mid-write, after which the next apply starts from scratch and collides with the resources that already exist. (If that happens: restore `terraform.tfstate` from `terraform.tfstate.backup`.)

## Verify

Follow the `next_steps` output:

```bash
terraform output next_steps
```

In short:

1. `eval "$(terraform output -raw eks_update_kubeconfig_command)"` then `kubectl -n datahub-executor get pods` - the worker pod should be `Running`.
2. `kubectl -n datahub-executor exec <pod> -- ls /mnt/secrets` - shows `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY` and `s3-ingest-credentials.json`.
3. Open `terraform output -raw remote_executors_url` in a browser - the `tf-example-aws-eks` pool shows one attached worker. (`terraform output -raw eks_console_url` gives the AWS Console view of the cluster: workloads, pods, events.)
4. Trigger a test ingest of *TF Example AWS - S3 CSV*: click **Run** on the page at `terraform output -raw ingestion_sources_url`, or trigger it from the terminal with `eval "$(terraform output -raw run_ingestion_command)"`. The first run can take a few minutes if the worker needs to install the `s3` plugin into a fresh venv. On success, `customers.csv` appears as a dataset on the `s3` platform.

## Cleanup

```bash
terraform destroy
```

Dependency ordering tears the worker Helm release down before the pool, the bucket is emptied (`force_destroy`) and the Secrets Manager secret is deleted immediately rather than scheduled (`recovery_window_in_days = 0`), so the example can be re-applied immediately. If a destroy fails partway, re-run it; if the worker release lingers, delete it first (`helm -n datahub-executor uninstall datahub-executor`) and re-run the destroy. Destroying the EKS cluster takes several minutes on its own.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Pod `ImagePullBackOff` | Wrong `executor_image_repository` path or an invalid/expired Cloudsmith token. Check `kubectl -n datahub-executor describe pod <pod>` (a `401 Unauthorized` from the registry login endpoint means the credential). Test the token directly: `curl -sS -o /dev/null -w '%{http_code}\n' -u "re:$TF_VAR_cloudsmith_token" "https://docker.datahub.com/v2/re/datahub-executor/tags/list"` - expect `200`. After correcting the token, run `terraform apply -replace=kubernetes_secret_v1.cloudsmith_pull` (write-only values are not diffed) and delete the pod to skip the backoff wait. |
| `Error: Unauthorized` or `the server has asked for the client to provide credentials` on kubernetes/helm resources | `aws eks get-token` ran without the credentials Terraform used, or as an identity with no cluster access. Check `aws sts get-caller-identity` in the same shell; the identity that ran the apply is granted cluster-admin by `enable_cluster_creator_admin_permissions`. |
| Pod `Pending` | CPU request does not fit the node. Keep `executor_cpu_request = "3"` on `m6i.xlarge` nodes (4 vCPU nodes have roughly 3.9 allocatable, less the daemonsets). |
| Pod `CreateContainerError` / `MountVolume.SetUp failed` | The CSI provider could not read the secret. Check the `eks-pod-identity-agent` addon is active, the `aws_eks_pod_identity_association` names the right namespace and service account (`kubectl -n datahub-executor get sa`), and the role policy names the secret's ARN. The provider logs are in `kubectl -n kube-system logs -l app=secrets-store-csi-driver-provider-aws`. |
| Worker running but not listed under Remote Executors | Wrong `datahub_gms_url` (must end in `/gms`) or an invalid Remote Executor token. Check the pod logs. |
| Ingestion run fails resolving `${...}` | For `TF_EXAMPLE_AWS_S3_BUCKET_NAME`, confirm the DataHub secret exists; for `S3_ACCESS_KEY_ID` / `S3_SECRET_ACCESS_KEY`, confirm step 2 above shows the mounted files (names are case-sensitive). |
| Ingestion run fails with `AccessDenied` from S3 | The access key resolved but the IAM user policy does not match the bucket. Check `aws_iam_user_policy.ingest_read_bucket` applied; a freshly created access key can also take a few seconds to become usable. |
