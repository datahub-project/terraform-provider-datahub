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
  value       = datahub_page_template.alternate.urn
}

# Two calls, no personal access token needed: log in for a session cookie, then
# set your own pointer. updateUserHomePageSettings needs no privilege and is
# hard-scoped to the caller, so a user can only ever change their own page.
output "switch_to_alternate_instructions" {
  description = "Commands the test user runs to adopt the alternate layout, using the password you supplied as TF_VAR_test_user_password."
  value = format(
    <<-EOT
    # NOTE: these two calls go to the DataHub FRONTEND, not to GMS. On
    # open-source DataHub they are different hosts -- frontend :9002, GMS :8080
    # -- and GMS returns 404 for both. On DataHub Cloud one host serves both, so
    # DATAHUB_FRONTEND_URL and DATAHUB_GMS_URL are the same value there.
    #
    #   export DATAHUB_FRONTEND_URL=http://localhost:9002   # Quickstart
    #   export DATAHUB_FRONTEND_URL="$DATAHUB_GMS_URL"      # DataHub Cloud
    #
    # 1. Log in and keep the session cookie.
    #    The password is read from the variable you already exported, so this
    #    pastes and runs as-is, and the credential never appears in the command
    #    itself, in your shell history, or in this output.
    #
    #    jq builds the body rather than string interpolation, so a password
    #    containing a quote or backslash cannot break the JSON.
    curl -sS -c cookies.txt -X POST "$DATAHUB_FRONTEND_URL/logIn" \
      -H 'Content-Type: application/json' \
      -d "$(jq -nc --arg u '%s' --arg p "$TF_VAR_test_user_password" '{username:$u,password:$p}')"

    # 2. Point yourself at the alternate layout
    curl -sS -b cookies.txt -X POST "$DATAHUB_FRONTEND_URL/api/v2/graphql" \
      -H 'Content-Type: application/json' \
      -d '%s'

    # To go back to the organisation default, replace the variables above with:
    #   {"input":{"removePageTemplate":true}}
    EOT
    , var.test_user_email,
    jsonencode({
      query     = "mutation u($input: UpdateUserHomePageSettingsInput!){ updateUserHomePageSettings(input:$input) }"
      variables = { input = { pageTemplate = datahub_page_template.alternate.urn } }
    })
  )
}

output "test_user_urn" {
  description = "URN of the created test user, if any. On DataHub Cloud this is derived from the email."
  value       = datahub_local_user_login.viewer.user_urn
}
