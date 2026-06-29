# Action pipeline: Dataplex glossary sync

Creates a DataHub Cloud **action pipeline** (automation) that propagates DataHub glossary terms back to the Google Dataplex catalog, and enumerates all action pipelines via the plural data source.

Action pipelines are **DataHub Cloud only** and are a newer DataHub Cloud capability. Because DataHub Cloud upgrades on its own release cadence, a release may occasionally affect this resource; fixes are handled in the provider. Pin the provider version for client-side stability and upgrade it to pick up fixes (including any needed for backend changes), and please open an issue if you hit one.

## Prerequisites

- A DataHub **Cloud** instance (action pipelines do not exist on OSS DataHub).
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` set, or a `provider "datahub"` block with `gms_url` / `gms_token`.
- A DataHub Secret named `GCP_SA_KEY` holding the service-account credential the recipe references (the recipe stores the `${GCP_SA_KEY}` placeholder, not the value).

## Run

```bash
terraform init
terraform apply
```

The recipe uses a `${GCP_SA_KEY}` placeholder (written as `$${GCP_SA_KEY}` in HCL so Terraform does not interpolate it). DataHub resolves it from Secrets when the action runs — never inline the credential.

## Verify

```bash
terraform output action_urn
```

In the DataHub Cloud UI, open **Settings → Integrations** to see the action pipeline and its run status.

## Import an existing pipeline

Action pipelines created via the UI or other tooling can be adopted by URN (full URN or bare `action_id`):

```bash
terraform import datahub_action_pipeline.example urn:li:dataHubAction:<id>
```

Or enumerate them all for bulk import:

```bash
terraform output all_action_pipeline_urns
```

## Cleanup

```bash
terraform destroy
```
