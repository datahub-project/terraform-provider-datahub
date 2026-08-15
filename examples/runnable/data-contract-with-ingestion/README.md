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

## Cleanup, and the part that needs you

`terraform destroy` removes the contract, the assertion and the ingestion source. It does **not** remove the dataset, because Terraform never created it — the ingestion run did.

```bash
terraform destroy

# then remove the dataset the contract pointed at:
eval "$(terraform output -raw cleanup_dataset_cli)"     # DataHub CLI
eval "$(terraform output -raw cleanup_dataset_curl)"    # or curl
```

Both commands are emitted complete, with the URN already substituted.

**The demo pack contains nine datasets, not one.** The outputs clean up only the one the contract used. To sweep the rest, list them and delete each:

```bash
datahub delete --platform hive --hard
datahub delete --platform kafka --hard
datahub delete --platform hdfs --hard
```

Run those against a throwaway instance only. On a shared instance they will remove datasets on those platforms that you did not create.
