terraform {
  required_version = ">= 1.11"
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.4.0"
    }
    http = {
      source  = "hashicorp/http"
      version = "~> 3.0"
    }
  }
}

provider "datahub" {
  # Credentials from environment variables:
  #   DATAHUB_GMS_URL   - DataHub GMS endpoint (e.g. https://your-instance.acryl.io/gms)
  #   DATAHUB_GMS_TOKEN - personal access token with Manage Domains privilege
}

# Fetch the full FIBO repository file tree from GitHub.
# The 3-level domain hierarchy (Domain/Module/Leaf) is encoded directly in the
# directory structure -- no pre-generated data file required.
# License: MIT, Copyright (c) 2020 Enterprise Data Management Council
data "http" "fibo_tree" {
  url = "https://api.github.com/repos/edmcouncil/fibo/git/trees/master?recursive=1"
  request_headers = {
    Accept     = "application/vnd.github.v3+json"
    User-Agent = "terraform-datahub-fibo-example"
  }
}

locals {
  _tree = jsondecode(data.http.fibo_tree.response_body)

  # Fail fast if GitHub truncated the listing -- truncation would silently
  # drop domains. Current FIBO repo is ~1800 files, well under the 100k limit.
  _assert_complete = local._tree.truncated == false ? true : tobool(
    "GitHub API truncated the FIBO tree -- some domains may be missing. Add an Authorization header with a GITHUB_TOKEN to raise the rate limit, or file an issue."
  )

  # Ontology scaffolding domains -- not meaningful business domain labels.
  skip_domains = toset(["FND", "BP"])

  # Select all .rdf blobs at exactly depth 3 (Domain/Module/Leaf.rdf),
  # excluding infrastructure files identified by filename prefix.
  leaf_entries = [
    for e in local._tree.tree : {
      domain = split("/", e.path)[0]
      module = split("/", e.path)[1]
      leaf   = trimsuffix(split("/", e.path)[2], ".rdf")
    }
    if e.type == "blob"
    && endswith(e.path, ".rdf")
    && length(split("/", e.path)) == 3
    && !contains(local.skip_domains, split("/", e.path)[0])
    && !startswith(split("/", e.path)[2], "All")
    && !startswith(split("/", e.path)[2], "Metadata")
    && !startswith(split("/", e.path)[2], "About")
    && !strcontains(split("/", e.path)[2], "Individuals")
  ]

  # Apply optional domain filter. Empty list (default) includes all domains.
  filtered = length(var.domains_filter) == 0 ? local.leaf_entries : [
    for e in local.leaf_entries : e
    if contains(var.domains_filter, e.domain)
  ]

  # Level 1: unique top-level FIBO domains (e.g. SEC, DER, LOAN).
  fibo_domains = toset([for e in local.filtered : e.domain])

  # Level 2: unique domain/module pairs (e.g. SEC/Debt, DER/CreditDerivatives).
  fibo_modules = {
    for key in toset([for e in local.filtered : "${e.domain}/${e.module}"]) :
    key => {
      domain = split("/", key)[0]
      module = split("/", key)[1]
    }
  }

  # Level 3: individual ontology leaf nodes (e.g. SEC/Debt/Bonds).
  fibo_leaves = {
    for e in local.filtered :
    "${e.domain}/${e.module}/${e.leaf}" => e
  }
}

# Level 1 -- root DataHub domains, one per FIBO top-level domain.
resource "datahub_domain" "fibo_domain" {
  for_each  = local.fibo_domains
  domain_id = "tf-example-fibo-${lower(each.key)}"
  name      = each.key
}

# Level 2 -- module domains nested under their top-level domain.
# The parent_domain reference gives Terraform the dependency edge so domains
# are created before modules and destroyed after them.
resource "datahub_domain" "fibo_module" {
  for_each      = local.fibo_modules
  domain_id     = "tf-example-fibo-${lower(each.value.domain)}-${lower(each.value.module)}"
  name          = each.value.module
  parent_domain = datahub_domain.fibo_domain[each.value.domain].urn
}

# Level 3 -- leaf ontology domains nested under their module.
resource "datahub_domain" "fibo_leaf" {
  for_each      = local.fibo_leaves
  domain_id     = "tf-example-fibo-${lower(each.value.domain)}-${lower(each.value.module)}-${lower(each.value.leaf)}"
  name          = each.value.leaf
  parent_domain = datahub_domain.fibo_module["${each.value.domain}/${each.value.module}"].urn
}
