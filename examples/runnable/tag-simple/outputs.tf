output "pii_tag_urn" {
  description = "URN of the PII tag."
  value       = datahub_tag.pii.urn
}

output "verified_tag_urn" {
  description = "URN of the Verified tag."
  value       = datahub_tag.verified.urn
}

output "deprecated_tag_urn" {
  description = "URN of the Deprecated tag."
  value       = datahub_tag.deprecated.urn
}

output "pii_tag_lookup_name" {
  description = "Display name of the PII tag as returned by the singular data source."
  value       = data.datahub_tag.pii_lookup.name
}

output "all_tag_urns" {
  description = "URNs of all tags returned by the datahub_tags data source (includes tags created outside this example)."
  value       = data.datahub_tags.all.urns
}

output "verify_url" {
  description = "DataHub UI URL to verify the created tags."
  value       = "${var.datahub_gms_url}/tags"
}
