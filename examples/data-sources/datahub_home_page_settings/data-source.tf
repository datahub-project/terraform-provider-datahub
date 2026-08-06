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
      # A module DataHub bootstraps; referenced, not created.
      modules = ["urn:li:dataHubPageModule:top_domains"]
    },
  ]
}

output "current_home_page_template" {
  value = data.datahub_home_page_settings.current.default_template_urn
}
