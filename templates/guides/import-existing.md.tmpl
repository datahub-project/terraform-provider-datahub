---
page_title: "Importing existing DataHub resources"
subcategory: ""
description: |-
  How to bring an existing DataHub deployment under Terraform management without recreating resources.
---

# Importing existing DataHub resources

If you have a DataHub deployment that was configured through the UI or the `datahub` CLI before you started using this provider, you can bring those resources under Terraform management without deleting and recreating them. Terraform calls this _importing_.

There are two paths:

- **CLI path** -- `datahub-tf-extract enumerate` extracts your DataHub resources as Terraform configuration automatically. Recommended for brownfield deployments with more than a handful of resources. You then run `terraform apply` on the output to complete the import into Terraform state.
- **Manual path** -- write the import blocks yourself using the enumeration data sources. Useful when you want to import a specific subset, or when you prefer full control over the generated HCL.

Both paths result in the same output: a Terraform configuration that matches your existing DataHub state, with no changes planned on the next `terraform plan`.

---

## CLI path

### Prerequisites

- Terraform 1.11 or later (required for WriteOnly attribute support)
- `datahub-tf-extract` binary (see installation options below)
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` set in your environment

#### Installing datahub-tf-extract

**Option 1: mise (recommended)**

If you use [mise](https://mise.jdx.dev), add the following to your `mise.toml` and run `mise install`:

```toml
[tools]
"ubi:datahub-project/terraform-provider-datahub" = { version = "0.7.0", exe = "datahub-tf-extract", matching = "tools-datahub-tf-extract" }
```

This pins the CLI to a specific version, keeps it in sync with the provider version your project uses, and requires no manual PATH management.

**Option 2: GitHub releases page**

Download the `tools-datahub-tf-extract_<version>_<os>_<arch>.zip` archive for your platform from the [GitHub releases page](https://github.com/datahub-project/terraform-provider-datahub/releases), unzip it, and move the binary to a directory on your PATH:

```shell
unzip tools-datahub-tf-extract_0.7.0_darwin_arm64.zip
mv datahub-tf-extract_v0.7.0 /usr/local/bin/datahub-tf-extract
```

**Option 3: build from source**

```shell
git clone https://github.com/datahub-project/terraform-provider-datahub.git
cd terraform-provider-datahub
make install
# binary written to ./bin/datahub-tf-extract
```

### Step 1: enumerate and generate

```shell
datahub-tf-extract enumerate --output ./import
```

This command:

1. Connects to DataHub and lists all resources of each supported type.
2. Writes `import.tf` with one `import {}` block per resource.
3. Runs `terraform init` and `terraform plan -generate-config-out=generated.tf` to generate the resource blocks.
4. Post-processes `generated.tf` to handle write-only attributes (see below).
5. Writes `variables.tf` for any sensitive values that cannot be recovered from state.
6. Writes `IMPORT_README.md` with next steps.

To limit the import to specific resource types:

```shell
datahub-tf-extract enumerate --output ./import --types datahub_secret,datahub_connection
```

To write `import.tf` only without running terraform (useful for inspecting URN enumeration):

```shell
datahub-tf-extract enumerate --output ./import --skip-terraform
```

### Step 2: fill in sensitive values

If your DataHub deployment includes secrets or connections, `variables.tf` will be present. Open `IMPORT_README.md` for the exact list. Create `terraform.tfvars` (it is gitignored automatically) and fill in the values:

```hcl
# terraform.tfvars -- do not commit this file
datahub_secret_bq_creds_value = "the-actual-secret-value"
```

For connections, you also need to add the appropriate platform block to the generated connection resource in `generated.tf`. The post-processor inserts a commented stub showing the available platform types -- uncomment and fill in the one that matches your connection.

### Step 3: verify

```shell
terraform -chdir=./import plan
```

A clean import produces output like:

```
No changes. Your infrastructure matches the configuration.
```

If terraform reports planned changes, they typically indicate:

- A write-only attribute whose variable value does not match the currently stored secret (Terraform cannot detect drift here by design -- the value was never in state).
- An attribute that DataHub returns in a normalized form that differs from what was imported (e.g. trailing whitespace in a recipe). Edit `generated.tf` to match.

### Step 4: adopt the state

Copy the files from `./import` into your main Terraform working directory (or move the entire directory and add a `backend` block). Run `terraform apply` -- it is a no-op that adopts the resources into your state file without making any changes to DataHub.

---

## Manual path

Use this path when you want to import a specific subset of resources or prefer to write the HCL yourself.

### Step 1: discover URNs

Use the enumeration data sources to list the URNs of existing resources:

```terraform
data "datahub_secrets" "all" {}
data "datahub_connections" "all" {}
data "datahub_ingestion_sources" "all" {}
```

After `terraform apply`, inspect the output:

```shell
terraform output -json | jq '.datahub_secrets_all.value.urns'
```

Or reference the URNs directly in other expressions:

```terraform
output "secret_urns" {
  value = data.datahub_secrets.all.urns
}
```

### Step 2: write import blocks

For each resource you want to import, add an `import {}` block and an empty `resource {}` block:

```terraform
import {
  to = datahub_secret.bq_creds
  id = "urn:li:dataHubSecret:bq-service-account"
}

resource "datahub_secret" "bq_creds" {}
```

### Step 3: generate configuration

```shell
terraform plan -generate-config-out=generated.tf
```

Terraform writes a `generated.tf` file containing the resource blocks populated from DataHub state. Review and move these blocks into your main configuration.

**Important:** this command is experimental in Terraform and may exit with a non-zero code when write-only attributes are present (see below). The file is still written and usable.

### Step 4: handle write-only attributes

Several attributes in this provider are write-only: Terraform sends them to DataHub on apply but DataHub never returns them in read operations. In `generated.tf`, these appear as:

```terraform
value = null # sensitive
```

You cannot leave these as `null` in your configuration -- Terraform will error on the next plan. Replace each one with the real value (or a variable reference):

```terraform
value            = var.bq_creds_value
value_wo_version = 1
```

Also add a matching `variable` block:

```terraform
variable "bq_creds_value" {
  type      = string
  sensitive = true
}
```

Affected attributes by resource type:

| Resource | Attribute | Action required |
|---|---|---|
| `datahub_secret` | `value` | Replace `null # sensitive` with the real secret value or a `var.*` reference; set `value_wo_version = 1` |
| `datahub_connection` | `config_wo_version` | Set to `1`; add the appropriate platform block (the generated file contains no platform block because the config is encrypted at rest) |

### Step 5: verify and apply

```shell
terraform plan   # should show no changes after filling in write-only values
terraform apply  # no-op; adopts resources into state
```

---

## Resource-specific notes

### datahub_secret

The `value` attribute is write-only and was never stored in Terraform state. After import:

- Set `value` in your config (or use a `var.*` reference pointing at a `terraform.tfvars` entry).
- Set `value_wo_version = 1`.
- Run `terraform apply` to record the version counter in state. Subsequent rotations work by incrementing the counter.

See [datahub_secret resource docs](../resources/secret.md) for rotation details.

### datahub_connection

The connection configuration blob is encrypted at rest in DataHub. After import:

- `name` and `platform` are populated from state.
- No platform block appears in `generated.tf` (all platform-specific fields are write-only).
- Add the correct platform block for your connection type and fill in the credentials.
- Set `config_wo_version = 1`.

The `datahub-tf-extract` CLI appends a commented stub listing all available platform block types to each generated connection resource.

See [datahub_connection resource docs](../resources/connection.md) for the full platform block schema.

### datahub_ingestion_source

Ingestion sources import cleanly with no write-only attributes. The full recipe JSON is stored in state and returned on read.

See [datahub_ingestion_source resource docs](../resources/ingestion_source.md).

### datahub_remote_executor_pool

Cloud-only. The `datahub-tf-extract` CLI skips remote executor pools when run against OSS DataHub and prints a notice. On DataHub Cloud, pools import cleanly with no write-only attributes.

See [datahub_remote_executor_pool resource docs](../resources/remote_executor_pool.md).

---

## Supported resource types

| Resource | CLI enumeration | Manual data source |
|---|---|---|
| `datahub_secret` | Yes | `datahub_secrets` |
| `datahub_connection` | Yes | `datahub_connections` |
| `datahub_ingestion_source` | Yes | `datahub_ingestion_sources` |
| `datahub_remote_executor_pool` | Yes (Cloud only) | `datahub_remote_executor_pool` |
