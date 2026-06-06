terraform {
  required_version = ">= 1.11"
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.6.0"
    }
  }
}

provider "datahub" {
  # Credentials from environment:
  #   DATAHUB_GMS_URL   - e.g. https://your-instance.acryl.io/gms  (or http://localhost:8080 for OSS)
  #   DATAHUB_GMS_TOKEN - personal access token
}

# ---------------------------------------------------------------------------
# Root domains
# ---------------------------------------------------------------------------

resource "datahub_domain" "finance" {
  domain_id   = "tf-example-finance"
  name        = "TF Example - Finance"
  description = "Financial metrics, accounting, and treasury data assets"
}

resource "datahub_domain" "engineering" {
  domain_id   = "tf-example-engineering"
  name        = "TF Example - Engineering"
  description = "Engineering platform, analytics, and infrastructure data assets"
}

# ---------------------------------------------------------------------------
# Child domains
#
# Referencing .urn (not a raw string) gives Terraform the dependency edge so
# parents are created first and destroyed last. DataHub refuses to hard-delete
# a domain that still has child domains, so correct ordering is required for
# terraform destroy to succeed.
# ---------------------------------------------------------------------------

resource "datahub_domain" "accounting" {
  domain_id     = "tf-example-accounting"
  name          = "TF Example - Accounting"
  description   = "Accounting standards, recognition principles, and ledger data"
  parent_domain = datahub_domain.finance.urn
}

resource "datahub_domain" "treasury" {
  domain_id     = "tf-example-treasury"
  name          = "TF Example - Treasury"
  description   = "Cash management, liquidity, and foreign exchange data"
  parent_domain = datahub_domain.finance.urn
}

resource "datahub_domain" "data_platform" {
  domain_id     = "tf-example-data-platform"
  name          = "TF Example - Data Platform"
  description   = "Data infrastructure, pipelines, and platform tooling"
  parent_domain = datahub_domain.engineering.urn
}

resource "datahub_domain" "analytics" {
  domain_id     = "tf-example-analytics"
  name          = "TF Example - Analytics"
  description   = "Product analytics, experimentation, and reporting"
  parent_domain = datahub_domain.engineering.urn
}
