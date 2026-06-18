output "action_id" {
  description = "The action pipeline id (URN suffix)."
  value       = datahub_action_pipeline.dataplex_glossary_sync.action_id
}

output "action_urn" {
  description = "The full DataHub URN of the action pipeline."
  value       = datahub_action_pipeline.dataplex_glossary_sync.urn
}

output "verify_url" {
  description = "Open the DataHub Cloud integrations/automations settings to verify the pipeline."
  value       = "Settings -> Integrations in the DataHub Cloud UI"
}

output "all_action_pipeline_urns" {
  description = "URNs of all action pipelines on the instance (for bulk import)."
  value       = data.datahub_action_pipelines.all.urns
}
