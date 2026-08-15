# metadata-test-simple

Demonstrates the `datahub_metadata_test` resource and the `datahub_metadata_tests` plural data source.

Creates two metadata tests -- declarative governance rules DataHub evaluates against catalog entities:

- **TF Example MDTest - Datasets Have Descriptions** -- every dataset must have a description, from the source system or edited in DataHub (an `or` over two properties).
- **TF Example MDTest - PROD Datasets Have an Owner** -- datasets whose `origin` is `PROD` must have at least one owner (an `on.conditions` block scoping the rule).

It then enumerates all metadata tests on the instance via the plural data source, which is the starting point for bulk import.

## What works where: OSS vs DataHub Cloud

Read this before wondering why nothing happens.

- **Creating, reading, updating, and destroying tests works on both OSS DataHub and DataHub Cloud.** The `test` entity and its mutations are part of the OSS API, and this whole example applies cleanly against an OSS instance.
- **Only DataHub Cloud evaluates tests.** OSS has no test engine: the definition is stored verbatim -- and unvalidated -- and nothing ever runs it. On OSS you will never see a pass/fail result on any dataset.
- **OSS may not even display the tests.** The OSS UI's test management page is gated behind the server's `testsConfig.enabled` flag, which defaults to false. On a stock OSS instance (including the Quickstart), the entities exist and are readable through the API, but the UI shows nothing. Use the `verify_command` output to confirm they exist.
- **On DataHub Cloud** the server validates the definition when the resource is created or updated, rejecting an invalid one with the validation messages, and the tests appear under **Govern → Tests** and start evaluating on the engine's schedule.

This example deliberately does **not** use the `datahub_metadata_test_validation` data source (which validates a definition at plan time): it is DataHub Cloud only, and including it would make the whole example fail on OSS. See its [registry documentation](https://registry.terraform.io/providers/datahub-project/datahub/latest/docs/data-sources/metadata_test_validation) for that pattern.

## Prerequisites

- Terraform >= 1.11
- A running DataHub instance (OSS or Cloud)
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` environment variables set
- A token whose user holds the **Manage Tests** platform privilege

## Run

```bash
export DATAHUB_GMS_URL=http://localhost:8080
export DATAHUB_GMS_TOKEN=your-token-here

terraform init
terraform apply
```

## Verify

From the terminal (works on OSS and Cloud):

```bash
eval "$(terraform output -raw verify_command)"
```

This reads one of the created tests back through the OpenAPI v3 entity endpoint and prints its stored aspects, including the definition JSON.

On DataHub Cloud, also navigate to **Govern → Tests** in the UI: both tests appear under the **Governance** category and begin evaluating against your datasets.

Note the `all_metadata_test_urns` output may not include the two new tests immediately after the first apply: the backing `listTests` query is served from the search index, which lags writes by a few seconds. A later `terraform plan` or `terraform refresh` shows them.

## Bulk import pattern

Tests created in the DataHub Cloud UI get random UUID ids. Use `datahub_metadata_tests` to enumerate them and adopt them into Terraform state without recreating them:

```hcl
data "datahub_metadata_tests" "existing" {}

import {
  for_each = toset(data.datahub_metadata_tests.existing.urns)
  id       = each.value
  to       = datahub_metadata_test.imported[each.value]
}

resource "datahub_metadata_test" "imported" {
  for_each        = toset(data.datahub_metadata_tests.existing.urns)
  name            = "placeholder" # replaced on first plan+apply after import
  category        = "placeholder"
  definition_json = "{}"
}
```

## Cleanup

```bash
terraform destroy
```

This hard-deletes both tests. On DataHub Cloud, test results already recorded on datasets keep referencing the deleted URNs until the next evaluation run recomputes them; on OSS, where nothing runs tests, no results exist to clean up.
