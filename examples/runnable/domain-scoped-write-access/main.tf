terraform {
  required_version = ">= 1.11"

  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.30.0"
    }
  }
}

provider "datahub" {
  # Credentials from environment:
  #   DATAHUB_GMS_URL   - e.g. https://your-instance.acryl.io
  #   DATAHUB_GMS_TOKEN - personal access token. Needs MANAGE_POLICIES,
  #                       MANAGE_USERS_AND_GROUPS, MANAGE_USER_CREDENTIALS,
  #                       CREATE_DOMAINS (or MANAGE_DOMAINS) and CREATE_TAGS
  #                       (or MANAGE_TAGS).
}

# ---------------------------------------------------------------------------
# The question this example answers
# ---------------------------------------------------------------------------
#
# "Can everyone have read-only access to ALL of DataHub, with write privileges
# limited to a given domain?"
#
# Yes -- but not as a single named role. DataHub roles (Admin/Editor/Reader)
# are platform-wide and cannot be narrowed to a domain, and custom roles do
# not exist. The supported way to express it is two pieces that combine,
# because privileges from every matching policy are added together:
#
#   1. The built-in Reader role, for read access across the whole catalog.
#   2. A domain-scoped METADATA policy, for edit rights inside one domain.
#
# This example builds both pieces plus everything needed to verify them: a
# domain hierarchy (parent + child, to prove the sub-domain cascade), an
# unrelated domain (to prove the deny boundary), a steward group, a login
# user in that group, and a pre-existing tag for the stewards to apply.

# ---------------------------------------------------------------------------
# Domains
# ---------------------------------------------------------------------------

# The domain the stewards govern.
resource "datahub_domain" "finance" {
  domain_id   = "tf-example-steward-finance"
  name        = "TF Example Steward - Finance"
  description = "Finance assets governed by the finance stewards group"
}

# A sub-domain. A DOMAIN policy criterion matches the selected domain AND
# every domain nested beneath it, so assets here are writable by the stewards
# even though the policy names only the parent. Referencing .urn (not a raw
# string) gives Terraform the dependency edge, so the parent is created first
# and destroyed last.
resource "datahub_domain" "finance_billing" {
  domain_id     = "tf-example-steward-finance-billing"
  name          = "TF Example Steward - Finance Billing"
  description   = "Sub-domain of Finance; inherits the stewards' write scope"
  parent_domain = datahub_domain.finance.urn
}

# An unrelated domain the stewards can read but never edit.
resource "datahub_domain" "marketing" {
  domain_id   = "tf-example-steward-marketing"
  name        = "TF Example Steward - Marketing"
  description = "Outside the stewards' write scope; read-only to them"
}

# ---------------------------------------------------------------------------
# Group and test user
# ---------------------------------------------------------------------------

# One group per governed domain is the pattern: name it for the domain, and
# repeat group + policy for each further domain that needs stewards. Where
# several domains share an identical privilege set, list them all in one
# policy's DOMAIN criterion instead (values within a criterion combine as OR).
resource "datahub_corp_group" "finance_stewards" {
  group_id    = "tf-example-steward-finance-stewards"
  name        = "TF Example Steward - Finance Stewards"
  description = "Members curate metadata inside the Finance domain"
}

# A native-login test user to run the verification checks as. Use an email
# address as the username: email-format usernames work on OSS (as a free-form
# identifier) and are required on Cloud (where the URN is derived from the
# email field).
resource "datahub_local_user_login" "steward" {
  username  = var.test_user_email
  email     = var.test_user_email
  full_name = "TF Example Steward - User"

  # Write-only: never written to Terraform state.
  initial_password = var.test_user_password
}

resource "datahub_corp_group_member" "steward" {
  group_urn = datahub_corp_group.finance_stewards.urn
  user_urn  = datahub_local_user_login.steward.user_urn
}

# ---------------------------------------------------------------------------
# Piece 1: Reader role -- read everything, edit nothing
# ---------------------------------------------------------------------------

data "datahub_role" "reader" {
  name = "Reader"
}

# Reader grants read access to the full catalog and carries no edit rights.
# DataHub allows one role per actor, so this is assigned per member (as the
# rollout guide prescribes), not to the group -- a member may already hold a
# role from elsewhere, and two assignments to one actor clobber each other.
#
# Note: fresh OSS instances also bootstrap an ACTIVE "All Users - View Entity
# Page" policy, so on a Quickstart everyone can read everything before this
# role does anything. On DataHub Cloud the Reader role is the mechanism.
resource "datahub_role_assignment" "steward_reader" {
  actor_urn = datahub_local_user_login.steward.user_urn
  role_urn  = data.datahub_role.reader.urn
}

# ---------------------------------------------------------------------------
# Piece 2: the domain-scoped metadata policy -- edit inside Finance only
# ---------------------------------------------------------------------------

# A tag that already exists for stewards to apply. Applying an existing tag is
# a metadata privilege and scopes to the domain cleanly; CREATING tags is a
# platform privilege that cannot be domain-scoped, so it stays with a central
# governance group (verification check 6 proves the stewards do not have it).
resource "datahub_tag" "reviewed" {
  tag_id      = "tf-example-steward-reviewed"
  name        = "TF Example Steward - Reviewed"
  description = "Applied by finance stewards once an asset has been curated"
}

resource "datahub_policy" "finance_stewards" {
  policy_id   = "tf-example-steward-finance-writers"
  name        = "TF Example Steward - Finance Domain Writers"
  type        = "METADATA"
  description = "Grants the finance stewards group curation rights on assets in the Finance domain and its sub-domains"

  # The steward privilege set: the curation work a domain owner realistically
  # needs, including column-level curation on datasets. Deliberately absent:
  # EDIT_ENTITY_DOMAINS -- it would let a steward move an asset OUT of the
  # domain, beyond the reach of this very policy. Withhold it when the domain
  # boundary is a control you need to hold steady.
  privileges = [
    "EDIT_ENTITY_OWNERS",              # assign and change ownership
    "EDIT_ENTITY_TAGS",                # apply and remove existing tags
    "EDIT_ENTITY_GLOSSARY_TERMS",      # attach and detach existing terms
    "EDIT_ENTITY_DOCS",                # write asset documentation
    "EDIT_ENTITY_DOC_LINKS",           # add supporting links and runbooks
    "EDIT_ENTITY_PROPERTIES",          # maintain custom/structured properties
    "EDIT_DEPRECATION_PRIVILEGE",      # mark assets deprecated
    "MANAGE_DATA_PRODUCTS",            # create data products inside the domain
    "EDIT_DATASET_COL_DESCRIPTION",    # document individual columns
    "EDIT_DATASET_COL_TAGS",           # tag columns (e.g. sensitivity)
    "EDIT_DATASET_COL_GLOSSARY_TERMS", # attach terms at column level
  ]

  actors = {
    groups = [datahub_corp_group.finance_stewards.urn]
  }

  # The domain is the only constraint, exactly as the rollout guide
  # recommends. DOMAIN matches the named domain and all of its descendants,
  # so assets in Finance Billing are covered too. An asset with NO domain
  # matches nothing and stays read-only to the stewards.
  #
  # A TYPE criterion (criteria combine with AND) would narrow this to chosen
  # entity types -- but note MANAGE_DATA_PRODUCTS is checked against the
  # *domain* entity itself, which a TYPE = ["dataset"] criterion would
  # exclude. Keep the filter domain-only unless you drop that privilege.
  resources = {
    filter = {
      criteria = [
        {
          field  = "DOMAIN"
          values = [datahub_domain.finance.urn]
        },
      ]
    }
  }
}

# ---------------------------------------------------------------------------
# Scratch datasets for the verification walkthrough
# ---------------------------------------------------------------------------

# The provider deliberately has no datahub_dataset resource (datasets come
# from ingestion, not Terraform), so the walkthrough seeds scratch datasets
# via the OpenAPI entity endpoint instead -- see the seed_dataset_commands
# output. These locals pin their URNs so every emitted command agrees.
locals {
  # Dataset name -> the domain it is seeded into (null = no domain, the
  # deliberately unmatchable fixture for verification check 5).
  seed_datasets = {
    "tf_example_steward.finance_ledger"      = datahub_domain.finance.urn
    "tf_example_steward.billing_invoices"    = datahub_domain.finance_billing.urn
    "tf_example_steward.marketing_campaigns" = datahub_domain.marketing.urn
    "tf_example_steward.unassigned_scratch"  = null
  }

  dataset_urn = {
    for name, _ in local.seed_datasets :
    name => "urn:li:dataset:(urn:li:dataPlatform:hive,${name},PROD)"
  }

  dataset_finance   = local.dataset_urn["tf_example_steward.finance_ledger"]
  dataset_billing   = local.dataset_urn["tf_example_steward.billing_invoices"]
  dataset_marketing = local.dataset_urn["tf_example_steward.marketing_campaigns"]
  dataset_orphan    = local.dataset_urn["tf_example_steward.unassigned_scratch"]
}

# The GraphQL probe bodies for the verification outputs, built with jsonencode
# so URNs and quotes can never break the JSON. Each runs as the STEWARD via
# the session cookie captured by the steward_login_commands output.
locals {
  steward_graphql = "curl -sS -b cookies.txt -X POST \"$DATAHUB_FRONTEND_URL/api/v2/graphql\" -H 'Content-Type: application/json' -d "

  add_tag_query = "mutation addTag($input: TagAssociationInput!) { addTag(input: $input) }"

  probe_graphql_commands = {
    read_marketing = format("%s'%s'", local.steward_graphql, jsonencode({
      query = format("query { dataset(urn: %q) { urn name } }", local.dataset_marketing)
    }))
    tag_finance = format("%s'%s'", local.steward_graphql, jsonencode({
      query     = local.add_tag_query
      variables = { input = { tagUrn = datahub_tag.reviewed.urn, resourceUrn = local.dataset_finance } }
    }))
    tag_billing = format("%s'%s'", local.steward_graphql, jsonencode({
      query     = local.add_tag_query
      variables = { input = { tagUrn = datahub_tag.reviewed.urn, resourceUrn = local.dataset_billing } }
    }))
    tag_marketing = format("%s'%s'", local.steward_graphql, jsonencode({
      query     = local.add_tag_query
      variables = { input = { tagUrn = datahub_tag.reviewed.urn, resourceUrn = local.dataset_marketing } }
    }))
    tag_orphan = format("%s'%s'", local.steward_graphql, jsonencode({
      query     = local.add_tag_query
      variables = { input = { tagUrn = datahub_tag.reviewed.urn, resourceUrn = local.dataset_orphan } }
    }))
    # The id is never created (the call is expected to fail); if you run it as
    # an admin by mistake, delete urn:li:tag:tf-example-steward-denied.
    create_tag = format("%s'%s'", local.steward_graphql, jsonencode({
      query     = "mutation createTag($input: CreateTagInput!) { createTag(input: $input) }"
      variables = { input = { id = "tf-example-steward-denied", name = "TF Example Steward - Denied" } }
    }))
  }
}
