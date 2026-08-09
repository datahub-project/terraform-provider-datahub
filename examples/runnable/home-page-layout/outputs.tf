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


# ---------------------------------------------------------------------------
# Publishing the alternate template
# ---------------------------------------------------------------------------

# DataHub has no query that lists page templates, so a user cannot discover this
# one. Handing out the URN is the only way it becomes reachable -- which makes
# this output the publication mechanism, not a convenience.
output "alternate_template_urn" {
  description = "URN of the opt-in alternate layout. Distribute this; nothing in DataHub will reveal it."
  value       = one(datahub_page_template.alternate[*].urn)
}

# Two calls, no personal access token needed: log in for a session cookie, then
# set your own pointer. updateUserHomePageSettings needs no privilege and is
# hard-scoped to the caller, so a user can only ever change their own page.
output "switch_to_alternate_instructions" {
  description = "Commands a user runs to adopt the alternate layout. Substitute their own password."
  value = var.create_alternate_template ? format(
    <<-EOT
    # 1. Log in and keep the session cookie
    curl -sS -c cookies.txt -X POST "$DATAHUB_GMS_URL/logIn" \
      -H 'Content-Type: application/json' \
      -d '{"username":"%s","password":"<your-password>"}'

    # 2. Point yourself at the alternate layout
    curl -sS -b cookies.txt -X POST "$DATAHUB_GMS_URL/api/v2/graphql" \
      -H 'Content-Type: application/json' \
      -d '%s'

    # To go back to the organisation default, replace the variables above with:
    #   {"input":{"removePageTemplate":true}}
    EOT
    , var.test_user_email,
    jsonencode({
      query     = "mutation u($input: UpdateUserHomePageSettingsInput!){ updateUserHomePageSettings(input:$input) }"
      variables = { input = { pageTemplate = one(datahub_page_template.alternate[*].urn) } }
    })
  ) : "Set create_alternate_template = true to publish an alternate layout."
}

output "test_user_urn" {
  description = "URN of the created test user, if any. On DataHub Cloud this is derived from the email."
  value       = one(datahub_local_user_login.viewer[*].user_urn)
}
