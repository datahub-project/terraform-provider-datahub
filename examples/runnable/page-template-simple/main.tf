terraform {
  required_version = ">= 1.11"

  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.23.0"
    }
  }
}

# Configure the DataHub provider.
# Credentials can also be supplied via DATAHUB_GMS_URL / DATAHUB_GMS_TOKEN
# environment variables.
provider "datahub" {}

# ---------------------------------------------------------------------------
# Modules -- the widgets a page is built from
# ---------------------------------------------------------------------------

# DataHub will only create four module types, each needing its matching params
# block: RICH_TEXT, LINK, ASSET_COLLECTION and HIERARCHY. Everything else
# (DOMAINS, DATA_PRODUCTS, OWNED_ASSETS, PLATFORMS ...) is a module DataHub
# bootstraps once per instance, which you reference rather than create.
resource "datahub_page_module" "welcome" {
  page_module_id = "tf-example-pagetpl-welcome"
  name           = "TF Example Page Template - Welcome"
  type           = "RICH_TEXT"

  params = {
    rich_text = {
      content = "This page is managed by Terraform."
    }
  }
}

resource "datahub_page_module" "docs_link" {
  page_module_id = "tf-example-pagetpl-docs"
  name           = "TF Example Page Template - Docs"
  type           = "LINK"

  params = {
    link = {
      link_url    = "https://docs.datahub.com"
      description = "DataHub documentation"
    }
  }
}

# ---------------------------------------------------------------------------
# The template -- the layout those modules sit in
# ---------------------------------------------------------------------------

# A template of its own id, NOT the organisation's home page. Nothing points at
# this template, so nobody sees it: DataHub seeds one default per instance and
# offers no API to aim the pointer elsewhere. Changing the page users see means
# managing that seeded template instead -- see the home-page-layout example.
#
# Because Terraform creates this template rather than adopting an existing one,
# `terraform destroy` deletes it in the ordinary way.
resource "datahub_page_template" "simple" {
  page_template_id = "tf-example-pagetpl-demo"
  surface_type     = "HOME_PAGE"

  # This resource owns the whole layout: every apply sends the complete set of
  # rows, so a module added to this template in the DataHub UI is removed on
  # the next apply.
  rows = [
    {
      modules = [datahub_page_module.welcome.urn]
    },
    {
      modules = [
        datahub_page_module.docs_link.urn,
        # A module DataHub bootstraps, referenced rather than created.
        "urn:li:dataHubPageModule:top_domains",
      ]
    },
  ]
}
