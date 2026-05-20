terraform {
  required_providers {
    datahub = {
      source = "registry.terraform.io/datahub-project/datahub"
    }
  }
}

provider "datahub" {
  # Credentials from environment:
  #   DATAHUB_GMS_URL   - e.g. https://your-instance.acryl.io
  #   DATAHUB_GMS_TOKEN - personal access token
}

# See README.md. Creates a real DataHub ingestion source pointing the
# "file" connector at https://ifconfig.me. Triggering this source from
# the DataHub UI surfaces the ingestion executor's egress IP in the
# run log -- useful when configuring source-system allow-lists.
resource "datahub_ingestion_source" "egress_ip_probe" {
  source_name = "Terraform Egress IP Probe"
  recipe = jsonencode({
    source = {
      type   = "file"
      config = { filename = "https://ifconfig.me" }
    }
    pipeline_name = "egress_ip_probe"
  })
}

output "ingestion_source_id" {
  value       = datahub_ingestion_source.egress_ip_probe.source_id
  description = "Short ID of the ingestion source."
}

output "source_urn" {
  value       = "urn:li:dataHubIngestionSource:${datahub_ingestion_source.egress_ip_probe.source_id}"
  description = "Full DataHub URN for this ingestion source. Use in API calls to trigger or inspect runs."
}
