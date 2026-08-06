output "template_urn" {
  description = "URN of the page template this example manages."
  value       = datahub_page_template.home.urn
}

output "template_id" {
  description = "Id of the page template this example manages."
  value       = datahub_page_template.home.page_template_id
}

output "module_urns" {
  description = "URNs of the modules laid out by the template, in row order. Two are created here; two are DataHub's bootstrapped defaults, referenced rather than created."
  value = [
    datahub_page_module.welcome.urn,
    local.domains_module,
    local.data_products_module,
    datahub_page_module.runbook.urn,
  ]
}

output "instance_default_template_urn" {
  description = "The template DataHub currently renders as everyone's home page."
  value       = data.datahub_home_page_settings.current.default_template_urn
}

output "is_the_live_home_page" {
  description = "Whether the managed template is the one users actually see."
  value       = datahub_page_template.home.urn == data.datahub_home_page_settings.current.default_template_urn
}

# Verify the layout DataHub stored, straight from the strongly-consistent read
# path. Run with: eval "$(terraform output -raw verify_command)"
output "verify_command" {
  description = "Reads back the stored rows for the managed template."
  value = format(
    "curl -sS -H \"Authorization: Bearer $DATAHUB_GMS_TOKEN\" %s | jq '.dataHubPageTemplateProperties.value.rows'",
    "\"$DATAHUB_GMS_URL/openapi/v3/entity/datahubpagetemplate/${datahub_page_template.home.urn}\"",
  )
}

# Where to look in the UI.
output "home_page_url" {
  description = "Open this to see the home page. Reflects the example only when is_the_live_home_page is true."
  value       = "$DATAHUB_GMS_URL/"
}
