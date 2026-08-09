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

# The standard DataHub views -- domains, data products, your assets, platforms
# and so on -- are NOT created here. DataHub bootstraps one of each per instance
# and its upsertPageModule resolver refuses to create them: only RICH_TEXT,
# LINK, ASSET_COLLECTION and HIERARCHY can be authored. Reference the
# bootstrapped URNs in the template rows below instead.
locals {
  domains_module       = "urn:li:dataHubPageModule:top_domains"
  data_products_module = "urn:li:dataHubPageModule:data_products"
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
        local.domains_module,
        local.data_products_module,
      ]
    },
    {
      modules = [datahub_page_module.runbook.urn]
    },
  ]
}

# ---------------------------------------------------------------------------
# An alternate layout users can opt into
# ---------------------------------------------------------------------------

# A second GLOBAL template. Nothing points at it, and DataHub has no query that
# lists templates -- so the only way anyone reaches it is by being told its URN.
# That is why it is published in outputs.tf: for this pattern the output is not
# a convenience, it is the entire discovery mechanism.
#
# Any user can then point themselves at it, with no privilege at all.
resource "datahub_page_template" "alternate" {
  count = var.create_alternate_template ? 1 : 0

  page_template_id = "tf-example-homepage-alternate"
  surface_type     = "HOME_PAGE"

  rows = [
    {
      modules = [datahub_page_module.welcome.urn]
    },
    {
      modules = [local.data_products_module]
    },
  ]
}

# ---------------------------------------------------------------------------
# A second user, to show who the organisation default actually reaches
# ---------------------------------------------------------------------------

# The point of this user is that they configured nothing. With no personal
# template of their own they see the organisation default -- which is how you
# tell that adopting the default template reached somebody other than the
# account that applied it.
#
# On DataHub Cloud the user's URN is derived from the email and ignores the
# username, so the two are set to the same value and the config works on both.
resource "datahub_local_user_login" "viewer" {
  count = var.create_test_user ? 1 : 0

  username  = var.test_user_email
  email     = var.test_user_email
  full_name = "TF Example Homepage Viewer"

  # Write-only: never stored in Terraform state.
  initial_password = var.test_user_password
}
