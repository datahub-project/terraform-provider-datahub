# data-contract-with-ingestion

Attaches a `datahub_data_contract` to a dataset that this configuration ingests, rather than to one you are assumed to already have.

```
datahub_ingestion_source (demo-data)     <- Terraform: a stored configuration
  |
  +- terraform_data + local-exec          <- NOT Terraform: an ingestion RUN
  |    trigger, then poll until the dataset exists
  |
  +- datahub_custom_assertion             <- Terraform
  +- datahub_data_contract                <- Terraform
```

## Why this example is shaped oddly

A data contract needs a dataset. Datasets are not created by this provider and never will be: the provider manages platform configuration, not the assets a platform discovers. There is no `datahub_dataset` resource.

That leaves ingestion, and ingestion splits into a noun and a verb. `datahub_ingestion_source` is the noun — a stored recipe — and the provider manages it happily. **Running it is the verb**, an asynchronous job with a duration and a completion state, and the provider exposes no resource for that, because a resource is a thing that exists rather than a thing that happens.

So the run is a `local-exec` provisioner. That is a liberty, taken deliberately and in one place only, and it is the reason this example is excluded from the automated live-example suite. It is here to teach the real workflow, not to be machine-verified.

The alternative — writing a dataset aspect directly at the API to conjure an entity — is what the provider's own acceptance tests do, and it is fine for a test. It would be dishonest in an example, because no user obtains a dataset that way.

## Prerequisites

- Terraform CLI 1.11 or later
- A running DataHub instance (OSS or Cloud) with the ingestion executor available. On Quickstart this is the `datahub-actions` container; if it is not running, the trigger succeeds and no dataset ever appears.
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` exported — the provisioner reads them from your shell, not from the provider block
- `curl` and `bash` on your `PATH`
- A token whose principal can manage ingestion (`MANAGE_INGESTION`), manage assertions, and edit the target dataset

## Apply

```bash
export DATAHUB_GMS_URL=https://your-instance.acryl.io
export DATAHUB_GMS_TOKEN=<personal-access-token>

terraform init
terraform apply
```

The apply blocks while the ingestion runs. On a warm Quickstart the dataset usually appears in well under a minute; the poll allows five.

## Verify

```bash
terraform output summary
```

Then open the dataset in the DataHub UI and select the **Quality** tab, where the contract and its assertion appear.

### "This contract has not yet been validated" is expected

The contract shows as unvalidated with *"No contract assertions have been run yet"*, and it will stay that way indefinitely. This is not a delay and not a defect.

A **custom assertion is externally evaluated**. DataHub stores the definition and never runs it, because the entire point of the type is that some other system does the checking and reports the outcome back. Only the typed assertion resources — freshness, volume, SQL, field and schema — are evaluated by DataHub itself, and all five are Cloud-only.

This example uses a custom assertion so that it works on open-source DataHub as well as Cloud. The cost is that nothing reports a result unless you do:

```bash
eval "$(terraform output -raw report_passing_result_command)"
```

Refresh the Quality tab and the contract shows as passing. That is the complete loop a real deployment runs on a schedule from its own data-quality tooling — this command stands in for that system.

Report a failure instead by editing the command and changing `type: SUCCESS` to `type: FAILURE`, which is worth doing once to see the contract go red.

## Cleanup, and the part that needs you

`terraform destroy` removes the contract, the assertion and the ingestion source. It does **not** remove the dataset, because Terraform never created it — the ingestion run did.

**`terraform output` stops working the moment you destroy.** Outputs are read from state, and destroy empties it — this is true even here, where the cleanup commands are built from a hardcoded URN and depend on no resource at all. So either capture them first:

```bash
terraform output -raw cleanup_contract_dataset_cli   # capture BEFORE destroying
terraform destroy
```

or skip the outputs and use the literal commands, which work at any time because the dataset URN is fixed in `main.tf` rather than computed:

```bash
terraform destroy

# DataHub CLI
datahub delete --urn 'urn:li:dataset:(urn:li:dataPlatform:hive,SampleHiveDataset,PROD)' --hard

# or curl, if the CLI is not installed
curl -sS -X DELETE -H "Authorization: Bearer $DATAHUB_GMS_TOKEN" \
  "$DATAHUB_GMS_URL/openapi/v3/entity/dataset/urn%3Ali%3Adataset%3A%28urn%3Ali%3AdataPlatform%3Ahive%2CSampleHiveDataset%2CPROD%29"
```

The outputs still exist and are worth reading before you destroy, but nothing in this section depends on them.

### The demo pack is much larger than the contract's dataset

`demo-data` loads DataHub's **entire sample estate**, not a dataset and not nine. Measured against a previously empty instance, one run created **58 entities across 16 types**: 6 datasets, 20 ML features, 7 ML primary keys, 5 ML feature tables, 2 containers, 2 charts, a dashboard, 2 tags, 3 glossary terms, a glossary node, 2 corp groups, 3 posts, a query, a data flow and an ML model.

**Do not sweep by platform or by name.** Two traps make that genuinely dangerous:

1. **`urn:li:corpuser:datahub` is in the sample pack** — DataHub's own admin account. Anything that deletes "everything the pack contains" will take it out.
2. **An entity the run merely *touched* looks identical to one it created.** On a shared instance an assertion that pre-dated the run by eight months carried the run's id alongside its original one; deleting by name would have removed it.

**Match on the ingestion run id instead.** Every aspect written by a run carries it in `systemMetadata`, so it distinguishes what the run *created* from what already existed:

```bash
# 1. Read the run id off any entity you know the run produced.
curl -sS -H "Authorization: Bearer $DATAHUB_GMS_TOKEN" \
  "$DATAHUB_GMS_URL/openapi/v3/entity/dataset/<url-encoded-urn>?systemMetadata=true" \
  | jq -r '[.. | objects | select(has("runId")) | .runId] | unique'

# 2. For each candidate URN, keep only those whose runIds are EXACTLY that one.
#    An entity listing two run ids pre-existed and must be left alone.

# 3. Delete the verified list in one pass.
datahub delete by-filter --urn-file ./verified-urns.txt --hard
```

On a throwaway Quickstart none of this matters — nuke the instance. It matters on any instance you share with someone else, which is precisely where the temptation to "just clean up quickly" is strongest.
