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

# This is the page your users see. Applying replaces it for everyone who has
# not personalised their own, and `terraform destroy` puts back the layout it
# had beforehand rather than deleting it.
#
# The id comes from the data source rather than being hardcoded, because
# home_default_1 is a bootstrap value rather than an API guarantee. There is no
# way to point DataHub at a different template, so managing the home page means
# managing this one.
#
# For a template that changes nothing users see, see the page-template-simple
# example instead.
resource "datahub_page_template" "home" {
  page_template_id = data.datahub_home_page_settings.current.default_template_id
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
# account that applied it. Without them this example is barely more than
# page-template-simple.
#
# The password comes from you, and is never stored. initial_password is
# write-only, so Terraform passes it to DataHub and keeps nothing.
#
# Terraform cannot generate one and show it to you without also keeping it: a
# root output is persisted in state by definition, and an ephemeral value
# cannot be a root output at all ("Ephemeral outputs are not allowed in context
# of a root module"). So every generate-and-display scheme ends with the
# credential in state permanently. Supplying it out of band avoids that
# entirely, and variables.tf fails loudly if you forget.
#
# No role is assigned, so the user gets whatever a new native user gets by
# default -- which is the point, since they are here to represent somebody who
# configured nothing.
#
# On DataHub Cloud the URN is derived from the email and ignores the username,
# so both are set to the same value and the config works on either flavour.
resource "datahub_local_user_login" "viewer" {
  username  = var.test_user_email
  email     = var.test_user_email
  full_name = "TF Example Homepage Viewer"

  # Write-only: never written to Terraform state.
  initial_password = var.test_user_password
}
