output "ingestion_source_id" {
  description = "Short ID of the ingestion source."
  value       = datahub_ingestion_source.egress_ip_probe.source_id
}

output "source_urn" {
  description = "Full DataHub URN -- use in API calls to trigger or inspect runs."
  value       = "urn:li:dataHubIngestionSource:${datahub_ingestion_source.egress_ip_probe.source_id}"
}

# Trigger command with the URN baked in. Copy-paste into a shell that has
# DATAHUB_GMS_URL and DATAHUB_GMS_TOKEN set. Save the returned execution
# request URN to check run status (see README.md).
output "trigger_command" {
  description = "curl command to trigger an ingestion run for this source."
  value = "curl -sS -X POST \"$DATAHUB_GMS_URL/api/graphql\" -H \"Authorization: Bearer $DATAHUB_GMS_TOKEN\" -H \"Content-Type: application/json\" -d '${jsonencode({query = "mutation Trigger($urn: String!) { createIngestionExecutionRequest(input: { ingestionSourceUrn: $urn }) }", variables = {urn = "urn:li:dataHubIngestionSource:${datahub_ingestion_source.egress_ip_probe.source_id}"}})}' | jq -r '.data.createIngestionExecutionRequest'"
}
