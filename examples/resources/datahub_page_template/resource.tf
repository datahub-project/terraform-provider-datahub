# Replace the organisation's home page with a two-row layout: a welcome banner
# across the top, then domains and data products side by side.
#
# Note the id. DataHub bootstraps one default home-page template on every
# instance and offers no API to point at a different one, so managing the home
# page means managing THAT template -- which is exactly what the DataHub UI's
# "edit default template" flow does. Creating a new template with some other id
# would succeed and then never be shown to anyone.
#
# Confirm the id on your own instance before relying on it, since it is a
# bootstrap value rather than an API guarantee:
#
#   curl -H "Authorization: Bearer $DATAHUB_GMS_TOKEN" \
#     "$DATAHUB_GMS_URL/openapi/v3/entity/globalsettings/urn:li:globalSettings:0" \
#     | jq -r '.globalSettingsInfo.value.homePage.defaultTemplate'
#
# Adopting an entity DataHub created is tidiest via an import block, so the
# first apply shows an update rather than a create:
#
#   import {
#     to = datahub_page_template.home
#     id = "home_default_1"
#   }
resource "datahub_page_template" "home" {
  page_template_id = "home_default_1"
  surface_type     = "HOME_PAGE"

  # This resource owns the whole layout: every apply sends the complete set of
  # rows, so a module added to this page in the DataHub UI is removed on the
  # next apply.
  rows = [
    {
      modules = [datahub_page_module.welcome.urn]
    },
    {
      # DataHub's bootstrapped modules, referenced rather than created -- only
      # RICH_TEXT, LINK, ASSET_COLLECTION and HIERARCHY can be authored.
      modules = [
        "urn:li:dataHubPageModule:top_domains",
        "urn:li:dataHubPageModule:data_products",
      ]
    },
  ]
}

resource "datahub_page_module" "welcome" {
  page_module_id = "org-home-welcome"
  name           = "Welcome"
  type           = "RICH_TEXT"

  params = {
    rich_text = {
      content = "Welcome to the data catalog."
    }
  }
}
