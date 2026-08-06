# Which template does this instance render as everyone's home page?
data "datahub_home_page_settings" "current" {}

# Use it to manage the home page without hardcoding the bootstrap id. Changing
# the home page means editing the template this points at -- DataHub offers no
# way to aim it somewhere else.
resource "datahub_page_template" "home" {
  page_template_id = data.datahub_home_page_settings.current.default_template_id
  surface_type     = "HOME_PAGE"

  rows = [
    {
      modules = [datahub_page_module.domains.urn]
    },
  ]
}

resource "datahub_page_module" "domains" {
  page_module_id = "home-settings-domains"
  name           = "Domains"
  type           = "DOMAINS"
}

output "current_home_page_template" {
  value = data.datahub_home_page_settings.current.default_template_urn
}
