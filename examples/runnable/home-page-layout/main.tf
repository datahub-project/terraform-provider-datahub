terraform {
  required_version = ">= 1.11"

  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.20.0"
    }
  }
}

# Configure the DataHub provider.
# Credentials can also be supplied via DATAHUB_GMS_URL / DATAHUB_GMS_TOKEN
# environment variables.
provider "datahub" {}

# ---------------------------------------------------------------------------
# What the instance currently shows as its home page
# ---------------------------------------------------------------------------

# Read-only. Changing the home page means editing the template this points at:
# DataHub seeds the pointer at bootstrap and offers no API to aim it elsewhere,
# and its own UI edits that template in place.
data "datahub_home_page_settings" "current" {}

# ---------------------------------------------------------------------------
# Modules -- the widgets that make up a page
# ---------------------------------------------------------------------------

resource "datahub_page_module" "welcome" {
  page_module_id = "tf-example-homepage-welcome"
  name           = "TF Example Homepage - Welcome"
  type           = "RICH_TEXT"

  params = {
    rich_text = {
      content = "Welcome. This home page is managed by Terraform."
    }
  }
}

# Most module types need no parameters: they render a standard DataHub view.
resource "datahub_page_module" "domains" {
  page_module_id = "tf-example-homepage-domains"
  name           = "TF Example Homepage - Domains"
  type           = "DOMAINS"
}

resource "datahub_page_module" "data_products" {
  page_module_id = "tf-example-homepage-data-products"
  name           = "TF Example Homepage - Data Products"
  type           = "DATA_PRODUCTS"
}

# A link module pointing wherever your team's runbook lives.
resource "datahub_page_module" "runbook" {
  page_module_id = "tf-example-homepage-runbook"
  name           = "TF Example Homepage - Runbook"
  type           = "LINK"

  params = {
    link = {
      link_url    = var.runbook_url
      description = "On-call procedures for the data platform"
    }
  }
}

# ---------------------------------------------------------------------------
# The template -- the layout those modules sit in
# ---------------------------------------------------------------------------

# By default this creates a template of its own, which applies and destroys
# cleanly but is NOT what anyone sees: nothing points at it. That default keeps
# the example safe to run and safe to tear down on a shared instance.
#
# Set adopt_default_template = true to manage the page users actually see. The
# id then comes from the data source above rather than being hardcoded, because
# home_default_1 is a bootstrap value rather than an API guarantee.
#
# Read the destroy caveat in the README before switching it on.
resource "datahub_page_template" "home" {
  page_template_id = var.adopt_default_template ? data.datahub_home_page_settings.current.default_template_id : "tf-example-homepage-demo"
  surface_type     = "HOME_PAGE"

  # This resource owns the whole layout. Every apply sends the complete set of
  # rows, so a module added to this page in the DataHub UI is removed on the
  # next apply.
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
    {
      modules = [datahub_page_module.runbook.urn]
    },
  ]
}
