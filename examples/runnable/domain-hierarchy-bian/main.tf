terraform {
  required_version = ">= 1.11"
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.4.0"
    }
  }
}

provider "datahub" {
  # Credentials from environment variables:
  #   DATAHUB_GMS_URL   - DataHub GMS endpoint (e.g. https://your-instance.acryl.io/gms)
  #   DATAHUB_GMS_TOKEN - personal access token with Manage Domains privilege
}

locals {
  bian = jsondecode(file("${path.module}/../../data/bian-service-landscape.json"))

  # Apply the filter: an empty list means include all business areas.
  selected_areas = length(var.business_areas_filter) == 0 ? local.bian.business_areas : [
    for a in local.bian.business_areas : a
    if contains(var.business_areas_filter, a.id)
  ]

  # Level 1: business areas (root domains, no parent).
  # Key: BIAN business-area id (e.g. "customers").
  business_areas = {
    for a in local.selected_areas : a.id => {
      name        = a.name
      description = a.description
    }
  }

  # Level 2: business domains (children of business areas).
  # Key: BIAN business-domain id (e.g. "customer-care").
  business_domains = merge([
    for a in local.selected_areas : {
      for bd in a.business_domains : bd.id => {
        name        = bd.name
        description = bd.description
        parent      = a.id
      }
    }
  ]...)

  # Level 3: service domains (children of business domains, leaf nodes).
  # Key: BIAN service-domain id (e.g. "customer-relationship-management").
  service_domains = merge([
    for a in local.selected_areas : merge([
      for bd in a.business_domains : {
        for sd in bd.service_domains : sd.id => {
          name        = sd.name
          description = sd.description
          parent      = bd.id
        }
      }
    ]...)
  ]...)
}

# Level 1: BIAN business areas as root DataHub domains.
resource "datahub_domain" "business_area" {
  for_each    = local.business_areas
  domain_id   = "tf-example-bian-ba-${each.key}"
  name        = each.value.name
  description = each.value.description
}

# Level 2: BIAN business domains nested under their business area.
# The parent_domain expression gives Terraform the dependency edge so business
# areas are created before their children and destroyed after them.
resource "datahub_domain" "business_domain" {
  for_each      = local.business_domains
  domain_id     = "tf-example-bian-bd-${each.key}"
  name          = each.value.name
  description   = each.value.description
  parent_domain = datahub_domain.business_area[each.value.parent].urn
}

# Level 3: BIAN service domains nested under their business domain.
resource "datahub_domain" "service_domain" {
  for_each      = local.service_domains
  domain_id     = "tf-example-bian-sd-${each.key}"
  name          = each.value.name
  description   = each.value.description
  parent_domain = datahub_domain.business_domain[each.value.parent].urn
}
