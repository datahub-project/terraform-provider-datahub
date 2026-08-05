# A rich-text module for a welcome banner.
resource "datahub_page_module" "welcome" {
  page_module_id = "welcome-banner"
  name           = "Welcome"
  type           = "RICH_TEXT"

  params = {
    rich_text = {
      content = "Welcome to the data catalog. Start with the Finance domain below."
    }
  }
}

# A link module pointing at an internal runbook.
resource "datahub_page_module" "runbook" {
  page_module_id = "data-runbook"
  name           = "Data platform runbook"
  type           = "LINK"

  params = {
    link = {
      link_url    = "https://example.internal/data-runbook"
      description = "On-call procedures and escalation paths"
    }
  }
}

# Many module types need no parameters at all -- they render a standard view.
resource "datahub_page_module" "domains" {
  page_module_id = "browse-domains"
  name           = "Domains"
  type           = "DOMAINS"
}

# A hierarchy module rooted on specific domains. Referencing the domain
# resource's urn rather than hardcoding a string gives Terraform the dependency
# edge, so the domain is created before the module that points at it.
resource "datahub_page_module" "finance_tree" {
  page_module_id = "finance-hierarchy"
  name           = "Finance hierarchy"
  type           = "HIERARCHY"

  params = {
    hierarchy_view = {
      asset_urns            = [datahub_domain.finance.urn]
      show_related_entities = true
    }
  }
}

resource "datahub_domain" "finance" {
  domain_id = "finance"
  name      = "Finance"
}
