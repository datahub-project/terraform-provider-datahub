# Example: Remote Executor on AWS ECS Fargate via CloudFormation (end-to-end)

**DataHub Cloud only.** Remote Executor pools, the executor image, and the Remote Executor UI do not exist in open-source DataHub.

> **COST WARNING.** Unlike most other runnable examples, this one creates billable AWS infrastructure: a Fargate task of 2 vCPU / 8 GB (roughly USD 85/month), a NAT gateway with its public IPv4 (roughly USD 36/month plus data processed), 50 GiB of task ephemeral storage (roughly USD 2.40/month for the 30 GiB above Fargate's included 20), and three Secrets Manager secrets and an S3 bucket (a couple of dollars). Call it USD 125/month. Billing starts at `terraform apply` and stops only when you run `terraform destroy`. Do not leave it running unattended.

This is the third Remote Executor example, and the first without Kubernetes. [`remote-executor-azure`](../remote-executor-azure/) and [`remote-executor-aws`](../remote-executor-aws/) deploy the executor onto AKS and EKS with Helm; this one deploys it onto **ECS Fargate** by driving the CloudFormation template DataHub publishes at [`acryldata/datahub-cloudformation`](https://github.com/acryldata/datahub-cloudformation) from an `aws_cloudformation_stack` resource. No cluster, no Helm, no CSI driver. The DataHub half (`datahub.tf`) and the seeded data (`s3.tf`) are kept as close to the EKS example as the different identifiers allow.

It stands up a complete, working Remote Executor deployment from nothing, in a single `terraform apply`:

1. A DataHub **Remote Executor pool** named `tf-example-ecs-fargate` (deliberately *not* the default pool).
2. A **VPC** with one private subnet (for the task) and one public subnet (for the NAT gateway the task reaches the internet through).
3. Three **Secrets Manager** secrets: the executor's DataHub token, and the two halves of a read-only IAM user's access key.
4. An **S3 bucket** with a seeded `customers.csv` object for the ingestion source to ingest.
5. The **Remote Executor** as an ECS cluster, Fargate task definition and service, created by the published CloudFormation template, attached to the `tf-example-ecs-fargate` pool.
6. An **ingestion source** (`s3` type) pinned to the pool, whose recipe exercises **both** secret-resolution paths:
   - `${TF_EXAMPLE_ECS_S3_BUCKET_NAME}` - a DataHub-managed secret, resolved via GMS
   - `${S3_ACCESS_KEY_ID}` and `${S3_SECRET_ACCESS_KEY}` - container environment variables, injected by ECS from Secrets Manager at task start through the template's `OptionalSecrets` parameter

All three secrets are load-bearing: if any fails to resolve, the ingestion run visibly fails.

## Architecture notes

**Why CloudFormation from Terraform, and why Fargate.** The Remote Executor operator guide publishes the ECS deployment as a CloudFormation template, and that template is what DataHub maintains and bumps with each executor release. It is Fargate-only and creates its own ECS cluster (`RequiresCompatibilities: [FARGATE]`, `LaunchType: FARGATE`, and an `AWS::ECS::Cluster` named after the stack, with no parameter to change any of them), so Fargate is not a choice this example makes but the shape the template ships; the only bring-your-own inputs are the VPC and subnet. An EC2 launch type or an existing cluster would need a different template. Driving it from Terraform means the DataHub pool, the secrets, the data and the executor all live in one configuration with one dependency graph, while the executor's internals stay exactly as DataHub ships them. DataHub also publishes a Terraform-native module (`git::https://github.com/acryldata/datahub-terraform-modules.git//remote-ingestion-executor`); if you would rather not have CloudFormation in the loop, that is the alternative, and the DataHub-side half of this example applies to it unchanged.

**The template is fetched, not copied.** `ecs.tf` reads the template at plan time from `raw.githubusercontent.com` at a pinned commit (`template_git_ref`). It is fetched rather than vendored because the upstream repository carries no licence, so there is no grant to redistribute it inside this Apache-2.0 repository; and it is pinned by commit because the repository has no release tags and `master` moves roughly weekly. Taking a template update is a deliberate edit to `template_git_ref` (and usually `executor_image_tag` with it). Plan therefore needs GitHub reachability, which is a smaller ask than the AWS reachability it already needs.

**Image path and the account allowlist.** The template fixes the image to DataHub's private ECR registry (`795586375822.dkr.ecr.us-west-2.amazonaws.com/datahub-executor`), pulled cross-account by the task's execution role; only the tag is yours to set. **DataHub must allowlist your AWS account ID for that registry before this will work** - ask your DataHub Cloud representative. No Cloudsmith token is involved, so this example needs one fewer credential than the Kubernetes ones. Check the allowlist *before* applying (see Prerequisites): a task that cannot pull its image is relaunched by ECS indefinitely, the template enables no deployment circuit breaker, so CloudFormation never sees a failure and Terraform's 30-minute create timeout fires with the stack stuck `CREATE_IN_PROGRESS`.

**Secret injection, not file mounts.** On Kubernetes the other two examples file-mount recipe secrets under `/mnt/secrets` with a CSI driver. ECS has no equivalent; what the template offers instead is `OptionalSecrets`, a comma-separated list of `NAME=secretArn` pairs that become container environment variables injected from Secrets Manager at task start, with the execution role granted `secretsmanager:GetSecretValue` on each ARN. The executor resolves `${NAME}` in recipes from three stores - environment, mounted files, and DataHub-managed secrets via GMS - in rising precedence, so the environment is the lowest-precedence source and a DataHub secret of the same name would win. Two consequences are worth knowing: the values are read once, at task start, so after rotating one you must redeploy the service (`terraform output -raw restart_executor_command`); and each secret must be a plain string, because the `:json-key::` selector ECS would need to pick a key out of a JSON document is part of the ARN string the template puts in the IAM policy, where it matches nothing. (The executor can also look secrets up at run time directly from Secrets Manager via `DATAHUB_EXECUTOR_AWS_SM_ENABLED`, but that needs Secrets Manager permission on the task role, which the template does not grant and this configuration cannot extend.)

**Secret hygiene.** No *copy* of a secret value is written to Terraform state beyond the resource that originates it: the DataHub secret uses the provider's write-only `value` attribute and all three Secrets Manager versions use `secret_string_wo`. The access key itself is a computed attribute of `aws_iam_access_key` and is in state, as is any credential Terraform creates; the write-only attributes stop it being copied again. The executor token is held in a Terraform-owned secret and passed to the template as `ExistingDataHubAccessTokenSecretArn` rather than through the template's own `DataHubAccessToken` parameter: `aws_cloudformation_stack` has no write-only parameters, and a `NoEcho` parameter reads back from CloudFormation as `****`, which would both put the token in state and show as permanent drift. Rotating any secret means bumping the corresponding `*_wo_version` counter and then redeploying the service. The flip side of write-only: Terraform cannot detect that a stored value differs from your variable, so if you applied with a wrong token, a plain `terraform apply` plans no change - force the rewrite with `terraform apply -replace=aws_secretsmanager_secret_version.executor_token`, then redeploy.

**Networking and region.** The template takes one existing subnet and assigns the task no public IP, so the task sits in a private subnet behind a NAT gateway; it needs outbound access only (ECR, GMS, and PyPI for ingestion plugins). One availability zone is the honest minimum for a single-subnet template. The region defaults to `us-west-2` because that is where the image registry lives: every task start pulls a multi-gigabyte image through the NAT gateway, and a cross-region default would add inter-region transfer to each one. This is an example shape, not a production one.

**Ephemeral storage.** The template defaults the task's ephemeral storage to 21 GiB. On EKS, nodes with the 20 GiB AL2023 default root volume evicted the executor within minutes once the multi-gigabyte image and the ingestion plugin venvs it installs on local disk had filled it, so this example passes `executor_ephemeral_storage_gib = 50` to the template's `TaskEphemeralStorageSizeInGiB` parameter rather than find out the same way twice. Fargate bills only the GiB above 20.

**Fixed names.** Every resource the template creates is named from the stack name (`tf-example-datahub-ecs`, `-service`, `-task`, `-log-group`, `-task-role`, `-execution-role`, `-container-sg`). IAM role names are global to the account, so the same stack name cannot exist in two regions of one account.

**No ECS Exec.** The template enables `ExecuteCommand` on the service, but its task role carries no `ssmmessages` permissions, so `aws ecs execute-command` does not work as shipped. Verification uses CloudWatch logs and the task definition instead.

## Prerequisites

- A DataHub Cloud instance and a personal access token with the *Manage Metadata Ingestion* privilege (for the provider).
- A second access token of type **Remote Executor** (DataHub UI: **Settings > Access Tokens > Generate new token > Remote Executor**) - this is what the executor uses to authenticate to GMS.
- **Your AWS account ID allowlisted on DataHub's executor ECR registry**, arranged through your DataHub Cloud representative. Confirm it before applying, with the same credentials Terraform will use:

  ```bash
  aws ecr batch-get-image --registry-id 795586375822 --repository-name datahub-executor \
    --image-ids imageTag=v2.3.1-cloud --region us-west-2 --query failures
  ```

  Expect `[]`. An `AccessDeniedException` means the account is not allowlisted yet; do not apply until it is.
- AWS credentials in the environment or a named profile (`AWS_PROFILE`), with permission to create VPC, CloudFormation, ECS, IAM, Secrets Manager, CloudWatch Logs and S3 resources. The `aws` CLI for the verification commands.
- Network access to `raw.githubusercontent.com` from wherever `terraform plan` runs.

Export credentials:

```bash
export DATAHUB_GMS_URL="https://your-instance.acryl.io"
export DATAHUB_GMS_TOKEN="<provider PAT>"
export TF_VAR_datahub_gms_url="$DATAHUB_GMS_URL/gms"
read -s TF_VAR_datahub_executor_token && export TF_VAR_datahub_executor_token
```

Optionally copy `terraform.tfvars.example` to `terraform.tfvars` for non-secret overrides (region, image tag, template commit).

## Apply

```bash
terraform init
terraform apply
```

Expect roughly 10-15 minutes: the VPC and NAT gateway take 2-3 minutes, the executor pool blocks until READY (30-90 seconds), and the CloudFormation stack waits for the service to reach a steady state - the first image pull is multi-gigabyte, so the task can sit in `PENDING` for several minutes, then `RUNNING` for a few more while the executor bootstraps and connects to GMS.

If the apply looks stuck on the stack, check the service before interrupting it (`terraform output -raw ecs_describe_service_command` works once the stack's outputs exist; before that, the CloudFormation console's Events tab). A single Ctrl-C is a graceful stop; a second Ctrl-C aborts immediately and can truncate the local state file mid-write, after which the next apply starts from scratch and collides with the resources that already exist. (If that happens: restore `terraform.tfstate` from `terraform.tfstate.backup`.)

## Verify

Follow the `next_steps` output:

```bash
terraform output next_steps
```

In short:

1. `eval "$(terraform output -raw ecs_describe_service_command)"` - `running` should be `1`.
2. `eval "$(terraform output -raw ecs_logs_tail_command)"` - the executor logs its connection to GMS and the pool it joined.
3. `eval "$(terraform output -raw ecs_task_definition_secrets_command)"` - lists `DATAHUB_GMS_TOKEN`, `S3_ACCESS_KEY_ID` and `S3_SECRET_ACCESS_KEY` (names only).
4. Open `terraform output -raw remote_executors_url` in a browser - the `tf-example-ecs-fargate` pool shows one attached executor. (`terraform output -raw ecs_console_url` gives the AWS Console view of the service.)
5. Trigger a test ingest of *TF Example ECS - S3 CSV*: click **Run** on the page at `terraform output -raw ingestion_sources_url`, or trigger it from the terminal with `eval "$(terraform output -raw run_ingestion_command)"`. The first run can take a few minutes if the executor needs to install the `s3` plugin into a fresh venv. On success, `customers.csv` appears as a dataset on the `s3` platform.

## Cleanup

```bash
terraform destroy
```

Dependency ordering deletes the stack (the service drains, then the cluster, roles and log group go) before the secrets, the VPC and the pool. The bucket is emptied (`force_destroy`) and the secrets are deleted immediately rather than scheduled (`recovery_window_in_days = 0`), so the example can be re-applied immediately. Stack deletion takes 3-5 minutes on its own. If a destroy fails partway, re-run it.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Apply hangs on `aws_cloudformation_stack.executor` and eventually times out; service shows `pending` tasks that keep stopping | The task cannot start. List them and read the reason: `aws ecs list-tasks --cluster tf-example-datahub-ecs --desired-status STOPPED --region <region>` then `aws ecs describe-tasks --cluster tf-example-datahub-ecs --tasks <arn> --region <region> --query 'tasks[].stoppedReason'`. `CannotPullContainerError` with an access-denied message means the account is not allowlisted on the registry (see Prerequisites); `ResourceInitializationError: unable to pull secrets` means a secret version did not exist yet or the ARN in `OptionalSecrets` is wrong. After fixing the cause, run `terraform apply -replace=aws_cloudformation_stack.executor` - a stack left in `CREATE_IN_PROGRESS` or `ROLLBACK_COMPLETE` cannot be updated, only replaced. |
| The apply died (lost terminal, killed process) while the stack was creating; `aws cloudformation describe-stacks --stack-name tf-example-datahub-ecs` shows `CREATE_COMPLETE` but the stack is not in state | CloudFormation finished the job on its own; Terraform never recorded it. Adopt it rather than recreate it: delete the stale `.terraform.tfstate.lock.info` sidecar (`terraform force-unlock` refuses on the local backend once the owning process is gone, and the real lock died with it), then `terraform import aws_cloudformation_stack.executor tf-example-datahub-ecs`. The next `terraform plan` shows a parameters-only in-place update - import cannot know which parameters the configuration manages, so it records none - and `terraform apply` sends an identical template and parameters, which CloudFormation reports as "No updates are to be performed" and the provider treats as success. The running executor is untouched throughout. |
| `Error: ... stack ... is in ROLLBACK_COMPLETE state and can not be updated` | A previous create failed and rolled back. The resource failure reasons are in the stack's Events; fix the cause, then `terraform apply -replace=aws_cloudformation_stack.executor`. |
| Postcondition failed on `data.http.executor_template` | `template_git_ref` does not name a commit that contains the template, or GitHub was unreachable. Check the URL in `ecs.tf` resolves in a browser. |
| Executor running but not listed under Remote Executors | Wrong `datahub_gms_url` (must end in `/gms`) or an invalid Remote Executor token. Check the logs (`ecs_logs_tail_command`); after correcting the token, `terraform apply -replace=aws_secretsmanager_secret_version.executor_token` (write-only values are not diffed) and `eval "$(terraform output -raw restart_executor_command)"`. |
| Ingestion run fails resolving `${...}` | For `TF_EXAMPLE_ECS_S3_BUCKET_NAME`, confirm the DataHub secret exists; for `S3_ACCESS_KEY_ID` / `S3_SECRET_ACCESS_KEY`, confirm step 3 above lists them (names are case-sensitive) and that the service was redeployed after any rotation. |
| Ingestion run fails with `AccessDenied` from S3 | The access key resolved but the IAM user policy does not match the bucket. Check `aws_iam_user_policy.ingest_read_bucket` applied; a freshly created access key can also take a few seconds to become usable. |
| `terraform plan` shows the stack changing every time | Should not happen: the only `NoEcho` parameter (`DataHubAccessToken`) is never passed. If it does, inspect `aws cloudformation describe-stacks --stack-name tf-example-datahub-ecs --query 'Stacks[0].Parameters'` for a `****` value and remove whichever parameter introduced it. |
