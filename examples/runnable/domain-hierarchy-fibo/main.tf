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

# Read the locally-generated FIBO hierarchy.
# Run `make fibo-data` once before terraform apply to clone the FIBO repo and
# produce this file. The file is gitignored; re-run `make fibo-data` to pick
# up new FIBO releases.
#
# License: FIBO is MIT-licensed by the EDM Council.
#   Copyright (c) 2020 Enterprise Data Management Council
#   https://github.com/edmcouncil/fibo/blob/master/LICENSE
locals {
  _fibo = jsondecode(file("${path.module}/.fibo-cache/fibo.json"))

  # Domains to exclude (ontology scaffolding -- already filtered by the script,
  # but repeated here for transparency).
  skip_domains = toset(["FND", "BP"])

  selected_domains = [
    for d in local._fibo.root.domains : d
    if !contains(local.skip_domains, d.code)
    && (length(var.domains_filter) == 0 || contains(var.domains_filter, d.code))
  ]

  # Level 1: top-level FIBO domains, keyed by id.
  fibo_domains = { for d in local.selected_domains : d.id => d }

  # Level 2: modules, keyed by "{domain_id}/{module_id}".
  fibo_modules = merge([
    for d in local.selected_domains : {
      for m in d.modules : "${d.id}/${m.id}" => merge(m, { domain_id = d.id })
    }
  ]...)

  # Level 3: leaf ontology nodes, keyed by "{domain_id}/{module_id}/{leaf_id}".
  fibo_leaves = merge([
    for d in local.selected_domains : merge([
      for m in d.modules : {
        for l in m.leaves : "${d.id}/${m.id}/${l.id}" => merge(l, {
          domain_id = d.id
          module_id = m.id
        })
      }
    ]...)
  ]...)
}

# Optional single root node above all domain nodes.
resource "datahub_domain" "fibo_root" {
  count       = var.create_root_node ? 1 : 0
  domain_id   = "tf-example-fibo-root"
  name        = local._fibo.root.name
  description = local._fibo.root.description
}

# Level 1 -- top-level FIBO domain nodes (e.g. Securities, Derivatives).
# When create_root_node is true, these are nested under the root; otherwise
# they are root-level DataHub domains.
resource "datahub_domain" "fibo_domain" {
  for_each      = local.fibo_domains
  domain_id     = "tf-example-fibo-${each.value.id}"
  name          = each.value.name
  description   = each.value.description
  parent_domain = var.create_root_node ? datahub_domain.fibo_root[0].urn : null
}

# Level 2 -- module nodes nested under their domain.
# The parent_domain reference gives Terraform the dependency edge so domains
# are created before modules and destroyed after them.
resource "datahub_domain" "fibo_module" {
  for_each      = local.fibo_modules
  domain_id     = "tf-example-fibo-${each.value.domain_id}-${each.value.id}"
  name          = each.value.name
  description   = each.value.description
  parent_domain = datahub_domain.fibo_domain[each.value.domain_id].urn
}

# Level 3 -- leaf ontology nodes nested under their module.
resource "datahub_domain" "fibo_leaf" {
  for_each      = local.fibo_leaves
  domain_id     = "tf-example-fibo-${each.value.domain_id}-${each.value.module_id}-${each.value.id}"
  name          = each.value.name
  description   = each.value.description
  parent_domain = datahub_domain.fibo_module["${each.value.domain_id}/${each.value.module_id}"].urn
}
