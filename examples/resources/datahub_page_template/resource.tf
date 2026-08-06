# A two-row home page: a welcome banner across the top, then domains and
# data products side by side.
#
# Reference each module's urn rather than writing the URN string by hand --
# Terraform then creates every module before the template that lays it out.
resource "datahub_page_template" "home" {
  page_template_id = "org-home"
  surface_type     = "HOME_PAGE"

  rows = [
    {
      modules = [datahub_page_module.welcome.urn]
    },
    {
      modules = [
        datahub_page_module.domains.urn,
        datahub_page_module.data_products.urn,
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

resource "datahub_page_module" "domains" {
  page_module_id = "org-home-domains"
  name           = "Domains"
  type           = "DOMAINS"
}

resource "datahub_page_module" "data_products" {
  page_module_id = "org-home-data-products"
  name           = "Data products"
  type           = "DATA_PRODUCTS"
}
