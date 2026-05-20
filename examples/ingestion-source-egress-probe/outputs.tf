output "ingestion_source_id" {
  description = "Short ID of the ingestion source."
  value       = datahub_ingestion_source.egress_ip_probe.source_id
}

output "source_urn" {
  description = "Full DataHub URN -- use in API calls to trigger or inspect runs."
  value       = "urn:li:dataHubIngestionSource:${datahub_ingestion_source.egress_ip_probe.source_id}"
}

output "next_steps" {
  description = "Post-apply summary and copy-pasteable follow-up commands."
  value       = <<-EOT

  Ingestion source created:

    Source ID:  ${datahub_ingestion_source.egress_ip_probe.source_id}
    Source URN: urn:li:dataHubIngestionSource:${datahub_ingestion_source.egress_ip_probe.source_id}
    DataHub UI: $DATAHUB_GMS_URL/ingestion

  To surface the executor's egress IP, trigger a run and inspect the result.
  Requires DATAHUB_GMS_URL and DATAHUB_GMS_TOKEN to be set in the shell.

  # 1. Trigger the run -- captures the execution request URN
  EXEC_URN=$(curl -sS -X POST "$DATAHUB_GMS_URL/api/graphql" \
    -H "Authorization: Bearer $DATAHUB_GMS_TOKEN" \
    -H "Content-Type: application/json" \
    -d '${jsonencode({query = "mutation Trigger($urn: String!) { createIngestionExecutionRequest(input: { ingestionSourceUrn: $urn }) }", variables = {urn = "urn:li:dataHubIngestionSource:${datahub_ingestion_source.egress_ip_probe.source_id}"}})}' \
    | jq -r '.data.createIngestionExecutionRequest')
  echo "Execution request: $EXEC_URN"

  # 2. Check the result (the run completes in seconds)
  curl -sS -X POST "$DATAHUB_GMS_URL/api/graphql" \
    -H "Authorization: Bearer $DATAHUB_GMS_TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"query\":\"query { executionRequest(urn: \\\"$EXEC_URN\\\") { result { status report } } }\"}" \
    | jq '.data.executionRequest.result'

  # The run fails to parse the ifconfig.me response -- the 'report' field
  # in the result contains the executor's egress IP. Add that IP to any
  # source-system network allow-list that DataHub's executor needs to reach.

  # To remove this ingestion source:
  # terraform destroy

  EOT
}
