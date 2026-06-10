# assertion-volume-sqlite

Demonstrates a DataHub volume assertion that evaluates against a locally-ingested SQLite dataset.

This example creates:
- A `datahub_ingestion_source` that profiles a local SQLite table and pushes the row count to DataHub Cloud as a `DatasetProfile` aspect.
- A `datahub_volume_assertion` that passes when the table has >= 100 rows.

**Requires DataHub Cloud.** Volume assertion monitors are not supported on OSS DataHub.

## Prerequisites

- Terraform >= 1.11
- [datahub CLI](https://datahubproject.io/docs/cli/) installed and configured (`datahub init` to create `~/.datahubenv`)
- Python 3 (available as `python3`)
- A DataHub Cloud instance with `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` set in your environment

## Setup and walkthrough

### 1. Seed 150 rows (initial state - assertion will PASS)

```bash
python3 fixtures/seed.py 150
```

### 2. Apply the Terraform config

```bash
terraform init
terraform apply
```

This registers the ingestion source and the assertion. The assertion does not run automatically until you trigger it.

### 3. Run ingestion to push the DatasetProfile to DataHub

```bash
datahub ingest -c <(terraform output -raw ingestion_source_urn | \
  datahub get --urn - --aspect dataHubIngestionSourceInfo | \
  python3 -c "import sys,json; print(json.load(sys.stdin)['dataHubIngestionSourceInfo']['value']['config']['recipe'])" | \
  python3 -c "import sys,json,yaml; print(yaml.dump(json.loads(sys.stdin.read())))")
```

Simpler alternative: trigger ingestion from the DataHub Cloud UI (Ingestion -> Run now on the `TF Example - SQLite Assertion Dataset` source).

### 4. Run the assertion (should PASS)

In DataHub Cloud UI: navigate to the `tf_test_data` dataset -> **Observe** tab -> click **Run** on the volume assertion.

Expected result: **PASS** (150 >= 100).

### 5. Drop the row count to 50 (assertion will FAIL)

```bash
python3 fixtures/seed.py 50
```

Re-run ingestion (same as step 3). Run the assertion again - expected result: **FAIL** (50 < 100).

### 6. Restore and verify PASS again

```bash
python3 fixtures/seed.py 150
```

Re-run ingestion. Run the assertion again - expected result: **PASS** (150 >= 100).

### 7. Clean up

```bash
terraform destroy
```

## Importing an existing assertion

If you have an existing volume assertion in DataHub, import it with:

```bash
terraform import datahub_volume_assertion.row_count_check <assertion-urn>
```

Note: evaluation schedule, source type, and mode cannot be read back from the DataHub entity API. After import, update those fields in your config to match the actual values and run `terraform plan` to confirm no drift.

## GCP demo migration

To migrate a `graphql_mutation` block from `sullivtr/graphql` to `datahub_volume_assertion`:

1. Find the existing assertion URN: `terraform state show graphql_mutation.your_assertion | grep '"id"'`
2. Add a `datahub_volume_assertion` block with matching parameters.
3. Import: `terraform import datahub_volume_assertion.your_assertion <urn>`
4. Run `terraform plan` - should show no changes for managed fields (evaluation schedule fields will show as empty strings after import).
5. Remove the `graphql_mutation` block and apply.
