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
"ubi:datahub-project/terraform-provider-datahub" = { version = "0.13.0", exe = "datahub-tf-extract", matching = "tools-datahub-tf-extract" }
```

This pins the CLI to a specific version, keeps it in sync with the provider version your project uses, and requires no manual PATH management.

**Option 2: GitHub releases page**

Download the `tools-datahub-tf-extract_<version>_<os>_<arch>.zip` archive for your platform from the [GitHub releases page](https://github.com/datahub-project/terraform-provider-datahub/releases), unzip it, and move the binary to a directory on your PATH:

```shell
unzip tools-datahub-tf-extract_0.13.0_darwin_arm64.zip
mv datahub-tf-extract_v0.13.0 /usr/local/bin/datahub-tf-extract
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

#### What enumeration includes and excludes

`enumerate` is deployment-wide: it discovers every instance of each supported type that the authenticated principal can see, not only the resources you (or one project) created. Two consequences are worth planning for:

- **System objects are excluded automatically where they can be identified.** DataHub Cloud provisions internal ingestion sources (`datahub-gc`, `datahub-usage-reporting`, `semantic-anchor`, ...) and internal/OAuth connections; these are filtered out so you never adopt platform-managed objects into Terraform. Assertions are filtered the same way: only `NATIVE` (author-as-code) assertions are enumerated, and only of the type/sub-shape each resource models -- ingested `EXTERNAL` assertions (e.g. dbt or Great Expectations tests) and `INFERRED` smart/AI assertions are never enumerated, since they are owned by the system that produces them.
- **Shared instances surface other people's objects.** On a shared or multi-tenant instance, enumeration will also list tags, glossary terms, policies, users, etc. created by others or by the UI. Always start with `--skip-terraform` and review `import.tf`, narrow with `--types`, and delete any `import {}` blocks you do not want before generating config. Importing the whole deployment is rarely what you want. Note that once you narrow this way you generate config yourself (`terraform plan -generate-config-out`, as in the Manual path below), which skips the post-processing the full `enumerate` run performs -- the connection platform-block stub, `variables.tf`, and `IMPORT_README.md` are not written, so handle write-only attributes manually (see Manual path, Step 4).

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

Copy the files from `./import` into your main Terraform working directory (or move the entire directory and add a `backend` block). Run `terraform apply` -- for resources with no write-only attributes this is a no-op that adopts them into your state file without making any changes to DataHub.

Write-only resources (`datahub_secret`, `datahub_connection`) are the exception: their `*_wo_version` rotation counter is import-as-`null` and triggers replacement when set, so the first post-import apply plans a one-time **replacement** (destroy + recreate) rather than a no-op. This is expected -- the value was never readable from DataHub, so it must be supplied and re-sent once. Because these resources use deterministic URNs, the recreated object keeps the same URN and references survive. See Step 5 of the Manual path.

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
terraform plan   # see note below for write-only resources
terraform apply  # adopts resources into state
```

For resources with no write-only attributes, `plan` shows no changes and `apply` is a no-op adopt. For write-only resources (`datahub_secret`, `datahub_connection`), `plan` instead shows a one-time **replacement**: the `*_wo_version` counter imports as `null`, and setting it to `1` forces a destroy + recreate (you will see `+ value_wo_version = 1 # forces replacement`). This is expected -- the value was never readable from DataHub, so it must be supplied in config and sent once. The supplied value must be the live secret's actual value; with a deterministic URN the recreated object keeps its URN, so references survive. Subsequent rotations work the same way: bump the counter to re-send a changed value.

---

## Migrating from another Terraform provider

The paths above assume the DataHub objects are **not yet managed by Terraform** (created via the UI, the `datahub` CLI, or the SDK). If they are **already tracked in a Terraform state** -- under a generic GraphQL provider, under `null_resource` + `local-exec` scripts, or under an earlier layout of this provider -- you are doing a provider *swap*, which needs a few extra steps. Plain `import` is not enough: the objects are already held by the old resources, so you must release that hold first.

This is rare, but the recipe is straightforward. For each object:

1. **Generate the new config** for the target `datahub_*` resources, exactly as in the CLI or Manual path above. If your Terraform project also manages unrelated resources (e.g. cloud infrastructure), generate in a *separate* directory so the generation step does not refresh them or require their credentials, then copy the resource blocks into your real configuration.
2. **Release the old management:** `terraform state rm <old.address>` for each resource that currently manages a DataHub object. This removes it from Terraform state only -- it does **not** delete the remote DataHub object.
3. **Import under the new resources:** `terraform import <new.address> <id>`. Prefer the imperative `terraform import` command (one resource at a time) over `import {}` blocks + `apply` here: it touches only the targeted resource, so it never refreshes or risks changing the rest of your project.
4. **Fix everything that referenced the old resources** -- not just the resource blocks. `outputs`, `locals`, and any cross-resource interpolation that pointed at the removed addresses will otherwise fail to plan (or, if the old blocks are still present, plan to recreate them). Repoint them at the new resources or remove them.
5. **Delete the old resource blocks** from your configuration.
6. **Verify** with a plan scoped to the new addresses: `terraform plan $(terraform state list | grep '^datahub_' | sed 's/^/-target=/')`. A clean swap reports `No changes`.

A few points worth knowing:

- **Order: import before you remove.** `terraform state rm` never deletes the remote object, so a gap where an object is briefly unmanaged is harmless -- but importing into the new resource *before* removing the old one keeps the old management as a safety net until the new hold is secured. Two addresses may reference the same remote object simultaneously; `terraform import` only guards its own target address.
- **Do not run a bare `plan`/`apply` mid-swap.** While both the old and new blocks exist in configuration, a full plan will try to create duplicates (or recreate the old resources you removed from state). Use only scoped, imperative commands until the swap is complete and the old blocks are deleted.
- **Back up your state first** (`cp terraform.tfstate terraform.tfstate.bak`); `state rm` and `import` are state surgery.
- **Labels:** imported resources are labelled by their import id (see the rename note in the CLI path) -- a cosmetic post-import cleanup, identical to a greenfield import.

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

Ingestion sources import with no write-only attributes; the full recipe JSON is stored in state and returned on read. The `recipe` attribute is compared by JSON semantic equality, so differences in whitespace or key order between your config and the form DataHub returns do not cause drift. One caveat specific to `import`: Terraform does not apply semantic equality during `plan`, so the very first plan immediately after import may show a one-time `# whitespace changes` diff on `recipe`. Run `terraform apply` once -- it is a no-op that normalizes state -- and subsequent plans are clean. Note that `enumerate` filters out DataHub Cloud's internal system ingestion sources, so they are never imported.

See [datahub_ingestion_source resource docs](../resources/ingestion_source.md).

### datahub_remote_executor_pool

Cloud-only. The `datahub-tf-extract` CLI skips remote executor pools when run against OSS DataHub and prints a notice. On DataHub Cloud, pools import cleanly with no write-only attributes.

See [datahub_remote_executor_pool resource docs](../resources/remote_executor_pool.md).

### Assertions (datahub_custom_assertion, datahub_freshness/volume/sql_assertion)

The CLI enumerates `datahub_custom_assertion` (CUSTOM-type assertions) and the Cloud-only monitor assertions `datahub_freshness_assertion`, `datahub_volume_assertion`, `datahub_sql_assertion`, `datahub_field_assertion`, and `datahub_schema_assertion`. The monitor enumerators are scoped to `source == NATIVE` (author-as-code monitors) and to the sub-shape each resource models -- `FIXED_INTERVAL`/`CRON`/`SINCE_THE_LAST_CHECK` freshness, `ROW_COUNT_TOTAL`/`ROW_COUNT_CHANGE` volume, `METRIC`/`METRIC_CHANGE` sql; field (FIELD_VALUES/FIELD_METRIC) and schema (DATA_SCHEMA) enumerate all NATIVE assertions of their type. Ingested `EXTERNAL` assertions (dbt, Great Expectations) and `INFERRED` smart/AI assertions are never enumerated, and a direct `terraform import` of a non-NATIVE assertion into one of these resources is refused with a clear diagnostic -- those assertions are owned by the system that produces them, like ingested lineage and profiles.

The monitor assertions' evaluation schedule, source type, and mode live in a separate Monitor entity; the provider reads that entity on import, so those fields are recovered automatically and an imported resource plans clean (supply the dataset-side assertion fields in config as usual).

Not auto-enumerated, even when NATIVE: DATA_JOB_RUN freshness (which targets a DataJob rather than a dataset). Import those by URN if you need them.

See the [datahub_custom_assertion](../resources/custom_assertion.md), [datahub_freshness_assertion](../resources/freshness_assertion.md), [datahub_volume_assertion](../resources/volume_assertion.md), [datahub_sql_assertion](../resources/sql_assertion.md), [datahub_field_assertion](../resources/field_assertion.md), and [datahub_schema_assertion](../resources/schema_assertion.md) resource docs.

---

## Supported resource types

`datahub-tf-extract` and the provider share a single import-target registry, so the CLI enumerates every resource type the provider can import. "CLI enumeration: Yes" means `datahub-tf-extract enumerate` discovers all instances of that type automatically; "No" means there is no list API for it (or it cannot be safely distinguished), so you supply URNs yourself via the manual path.

| Resource | CLI enumeration | Manual data source |
|---|---|---|
| `datahub_secret` | Yes | `datahub_secrets` |
| `datahub_connection` | Yes | `datahub_connections` |
| `datahub_ingestion_source` | Yes | `datahub_ingestion_sources` |
| `datahub_domain` | Yes | `datahub_domains` |
| `datahub_tag` | Yes | `datahub_tags` |
| `datahub_glossary_node` | Yes | `datahub_glossary_nodes` |
| `datahub_glossary_term` | Yes | `datahub_glossary_terms` |
| `datahub_structured_property` | Yes | `datahub_structured_properties` |
| `datahub_data_product` | Yes | `datahub_data_products` |
| `datahub_ownership_type` | Yes | `datahub_ownership_types` |
| `datahub_policy` | Yes | `datahub_policies` |
| `datahub_corp_group` | Yes | `datahub_corp_groups` |
| `datahub_corp_user` | Yes | `datahub_corp_user` |
| `datahub_custom_assertion` | Yes (CUSTOM-type assertions only) | `datahub_assertions` |
| `datahub_remote_executor_pool` | No (Cloud only; supply pool IDs) | `datahub_remote_executor_pool` |
| `datahub_freshness_assertion` | Yes (Cloud only; NATIVE, CRON/FIXED_INTERVAL/SINCE_THE_LAST_CHECK) | `datahub_assertions` |
| `datahub_volume_assertion` | Yes (Cloud only; NATIVE, ROW_COUNT_TOTAL/ROW_COUNT_CHANGE) | `datahub_assertions` |
| `datahub_sql_assertion` | Yes (Cloud only; NATIVE, METRIC/METRIC_CHANGE) | `datahub_assertions` |
| `datahub_field_assertion` | Yes (Cloud only; NATIVE, FIELD_VALUES/FIELD_METRIC) | `datahub_assertions` |
| `datahub_schema_assertion` | Yes (Cloud only; NATIVE) | `datahub_assertions` |
| `datahub_action_pipeline` | Yes (Cloud only; experimental) | `datahub_action_pipelines` |
| `datahub_corp_group_member` | No (relationship; import by composite ID) | -- |
| `datahub_role_assignment` | No (relationship; import by composite ID) | -- |
| `datahub_local_user_login` | No (import by user URN) | -- |
