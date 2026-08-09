output "template_urn" {
  description = "URN of the page template this example created."
  value       = datahub_page_template.simple.urn
}

output "module_urns" {
  description = "URNs of the modules the template lays out, in row order."
  value = [
    datahub_page_module.welcome.urn,
    datahub_page_module.docs_link.urn,
  ]
}

# Read the stored layout back through the strongly-consistent path.
# Run with: eval "$(terraform output -raw verify_command)"
output "verify_command" {
  description = "Reads back the rows DataHub stored for this template."
  value = format(
    "curl -sS -H \"Authorization: Bearer $DATAHUB_GMS_TOKEN\" %s | jq '.dataHubPageTemplateProperties.value.rows'",
    "\"$DATAHUB_GMS_URL/openapi/v3/entity/datahubpagetemplate/${datahub_page_template.simple.urn}\"",
  )
}

# This template is not the organisation's home page, so there is nothing to see
# in the UI. Point a user at it explicitly to view it -- no privilege needed,
# and it only affects the calling user.
output "view_as_yourself_command" {
  description = "Switches your own home page to this template so you can see it."
  value = format(
    "curl -sS -X POST -H \"Authorization: Bearer $DATAHUB_GMS_TOKEN\" -H 'Content-Type: application/json' -d '%s' \"$DATAHUB_GMS_URL/api/graphql\"",
    jsonencode({
      query     = "mutation u($input: UpdateUserHomePageSettingsInput!){ updateUserHomePageSettings(input:$input) }"
      variables = { input = { pageTemplate = datahub_page_template.simple.urn } }
    }),
  )
}

output "reset_yourself_command" {
  description = "Clears your personal page so you see the organisation default again."
  value = format(
    "curl -sS -X POST -H \"Authorization: Bearer $DATAHUB_GMS_TOKEN\" -H 'Content-Type: application/json' -d '%s' \"$DATAHUB_GMS_URL/api/graphql\"",
    jsonencode({
      query     = "mutation u($input: UpdateUserHomePageSettingsInput!){ updateUserHomePageSettings(input:$input) }"
      variables = { input = { removePageTemplate = true } }
    }),
  )
}
