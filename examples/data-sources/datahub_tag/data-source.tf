# Look up a tag by its id to reference its URN elsewhere, without managing the
# tag itself.
data "datahub_tag" "pii" {
  tag_id = "tf-example-pii"
}

output "pii_tag_urn" {
  description = "URN of the PII tag."
  value       = data.datahub_tag.pii.urn
}
