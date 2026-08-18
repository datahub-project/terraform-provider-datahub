terraform {
  required_version = ">= 1.11"

  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.24.0"
    }
  }
}

provider "datahub" {}

locals {
  # One of the datasets in DataHub's bootstrap demo pack. The pack is fixed, so
  # this URN is predictable -- which is what lets a contract point at it.
  dataset_urn = "urn:li:dataset:(urn:li:dataPlatform:hive,SampleHiveDataset,PROD)"

  # A dataset URN contains parentheses and commas, so it must be encoded before
  # it can be used as a path segment on the entity endpoint.
  dataset_urn_encoded = urlencode(local.dataset_urn)

  # datahub_ingestion_source exposes no urn attribute -- the only entity
  # resource in the provider that does not -- so the URN is composed from the
  # id we set. Deterministic precisely because source_id is set explicitly.
  ingestion_source_urn = "urn:li:dataHubIngestionSource:${datahub_ingestion_source.demo.source_id}"
}

# A data contract attaches to a dataset, and a dataset is something DataHub
# discovers by ingesting it. There is no datahub_dataset resource, and there
# will not be one: the provider manages platform configuration, not the assets
# a platform discovers.
#
# So this example ingests one rather than fabricating it. The demo-data source
# loads DataHub's bootstrap pack, which contains a fixed set of sample datasets
# with stable URNs, so the contract below has something real to point at.
resource "datahub_ingestion_source" "demo" {
  source_id   = "tf-example-contract-demo-data"
  source_name = "TF Example Contract - Demo Data"
  source_type = "demo-data"
  # "default" routes to the built-in executor on both OSS and Cloud.
  remote_executor_id = "default"

  # No cron_interval: this source exists to be run once, by the trigger below.
  recipe = jsonencode({
    source = {
      type   = "demo-data"
      config = {}
    }
  })
}

# The step Terraform cannot express as a resource.
#
# Creating an ingestion source is a noun -- a stored configuration. RUNNING it
# is a verb: an asynchronous job with a duration and a completion state, and
# the provider deliberately exposes no resource for it. So the run is a
# provisioner, and this is the liberty the example takes knowingly.
#
# It polls for the dataset rather than for the execution's status, because the
# dataset existing is the condition the contract actually depends on.
resource "terraform_data" "ingest" {
  # Re-run if the source changes, so an edited recipe is actually applied.
  triggers_replace = [local.ingestion_source_urn]

  provisioner "local-exec" {
    interpreter = ["/bin/bash", "-c"]
    command     = <<-EOT
      set -euo pipefail
      : "$${DATAHUB_GMS_URL:?set DATAHUB_GMS_URL}"
      : "$${DATAHUB_GMS_TOKEN:?set DATAHUB_GMS_TOKEN}"

      echo "Triggering ingestion for ${local.ingestion_source_urn}"
      curl -sS -X POST "$DATAHUB_GMS_URL/api/graphql" \
        -H "Authorization: Bearer $DATAHUB_GMS_TOKEN" \
        -H 'Content-Type: application/json' \
        -d '{"query":"mutation { createIngestionExecutionRequest(input:{ingestionSourceUrn:\"${local.ingestion_source_urn}\"}) }"}'

      echo
      echo "Waiting for the dataset to appear (up to 300s)"
      for i in $(seq 1 100); do
        code=$(curl -sS -o /dev/null -w '%%{http_code}' \
          -H "Authorization: Bearer $DATAHUB_GMS_TOKEN" \
          "$DATAHUB_GMS_URL/openapi/v3/entity/dataset/${local.dataset_urn_encoded}")
        if [ "$code" = "200" ]; then
          echo "Dataset present after $((i * 3))s"
          exit 0
        fi
        sleep 3
      done

      echo "Dataset did not appear within 300s." >&2
      echo "Check that the ingestion executor (datahub-actions) is running and" >&2
      echo "that the execution request above was accepted." >&2
      exit 1
    EOT
  }
}

# A contract needs at least one assertion to mean anything. A custom
# (externally evaluated) assertion works on open-source DataHub; every typed
# assertion resource is Cloud-only.
resource "datahub_custom_assertion" "freshness" {
  entity_urn     = local.dataset_urn
  assertion_type = "Data Contract Check"
  description    = "TF Example Contract - rows land daily"
  platform_urn   = "urn:li:dataPlatform:great-expectations"

  depends_on = [terraform_data.ingest]
}

resource "datahub_data_contract" "sample" {
  dataset_urn                 = local.dataset_urn
  data_quality_assertion_urns = [datahub_custom_assertion.freshness.urn]

  depends_on = [terraform_data.ingest]
}
