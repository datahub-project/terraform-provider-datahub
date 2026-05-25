# Ingestion Source Lookup Example

## What this example does

Reads back the `datahub-gc` built-in ingestion source using the `datahub_ingestion_source` data source.

`datahub-gc` is a system-managed garbage collection source present on every DataHub instance (both DataHub Cloud and OSS Quickstart). It exists before any Terraform is applied and should not be managed by Terraform -- making it an ideal demonstration of the data source pattern: look up an existing ingestion source by ID, regardless of how it was originally created.

This is a read-only operation. `terraform apply` reads the source and populates outputs. No DataHub resources are created or destroyed.

## Prerequisites

- `DATAHUB_GMS_URL` set to your DataHub instance URL (e.g. `https://your-instance.acryl.io`)
- `DATAHUB_GMS_TOKEN` set to a Personal Access Token with read access
- Terraform >= 1.11

## Run

```bash
export DATAHUB_GMS_URL=https://your-instance.acryl.io
export DATAHUB_GMS_TOKEN=<your-token>

terraform init
terraform apply
```

Expected output after a successful apply:

```
Apply complete! Resources: 0 added, 0 changed, 0 destroyed.

Outputs:

recipe_connector_type = "datahub-gc"
source_name           = "DataHub GC"
source_type           = "datahub-gc"
urn                   = "urn:li:dataHubIngestionSource:datahub-gc"
```

## Looking up other sources

Any ingestion source can be looked up by its `source_id`. For sources created via the DataHub UI, the ID appears in the browser URL:

```
https://<your-datahub>/ingestion/sources/<source_id>
```

Change `source_id = "datahub-gc"` to any ID from your instance.

## Using the recipe

The `recipe` attribute is returned as a JSON-encoded string. Use `jsondecode()` to access individual fields, as shown in `outputs.tf`:

```hcl
jsondecode(data.datahub_ingestion_source.gc.recipe).source.type
```

The recipe structure varies by connector type; refer to the [DataHub ingestion docs](https://datahubproject.io/docs/metadata-ingestion/) for the shape of each connector's config.
