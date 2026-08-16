terraform {
  required_version = ">= 1.11"
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.23.0"
    }
  }
}

provider "datahub" {
  # Credentials from environment:
  #   DATAHUB_GMS_URL   - e.g. https://your-instance.acryl.io/gms  (or http://localhost:8080 for OSS)
  #   DATAHUB_GMS_TOKEN - personal access token
}

# ---------------------------------------------------------------------------
# Root-level term groups
# ---------------------------------------------------------------------------

resource "datahub_glossary_node" "finance" {
  node_id     = "tf-example-glossary-finance"
  name        = "TF Example Glossary - Finance"
  description = "Financial metrics, accounting concepts, and revenue definitions"
}

resource "datahub_glossary_node" "customer" {
  node_id     = "tf-example-glossary-customer"
  name        = "TF Example Glossary - Customer"
  description = "Customer lifecycle, segmentation, and retention concepts"
}

# ---------------------------------------------------------------------------
# Second-level term groups (nested under a root node)
#
# Referencing .urn (not a raw string) gives Terraform the dependency edge so
# parents are created first and children are destroyed first. DataHub does not
# enforce a child guard on node deletion, so this ordering is the only safety
# net -- without it a parent could be deleted before its children.
# ---------------------------------------------------------------------------

resource "datahub_glossary_node" "accounting" {
  node_id     = "tf-example-glossary-accounting"
  name        = "TF Example Glossary - Accounting"
  description = "Accounting standards and recognition principles"
  parent_node = datahub_glossary_node.finance.urn
}

resource "datahub_glossary_node" "segmentation" {
  node_id     = "tf-example-glossary-segmentation"
  name        = "TF Example Glossary - Segmentation"
  description = "Customer segmentation and cohort analysis concepts"
  parent_node = datahub_glossary_node.customer.urn
}

# ---------------------------------------------------------------------------
# Glossary terms -- leaf nodes attached to their parent term group
#
# Three terms hang directly off root nodes (demonstrating shallow terms); two
# hang off second-level nodes (demonstrating deeper nesting). All five use
# .urn references so Terraform destroys terms before their parent nodes.
# ---------------------------------------------------------------------------

# Direct child of the Finance root node.
resource "datahub_glossary_term" "revenue" {
  term_id     = "tf-example-glossary-revenue"
  name        = "TF Example Glossary - Revenue"
  description = "Total revenue recognised in the reporting period before any deductions"
  parent_node = datahub_glossary_node.finance.urn
}

# Child of the Accounting sub-node (depth 2).
resource "datahub_glossary_term" "accrual" {
  term_id     = "tf-example-glossary-accrual"
  name        = "TF Example Glossary - Accrual"
  description = "Revenue or expense recorded when earned or incurred, regardless of cash movement"
  parent_node = datahub_glossary_node.accounting.urn
}

# Direct child of the Customer root node.
resource "datahub_glossary_term" "churn" {
  term_id     = "tf-example-glossary-churn"
  name        = "TF Example Glossary - Churn"
  description = "Rate at which customers discontinue their subscription or stop purchasing"
  parent_node = datahub_glossary_node.customer.urn
}

# Child of the Segmentation sub-node (depth 2).
resource "datahub_glossary_term" "cohort" {
  term_id     = "tf-example-glossary-cohort"
  name        = "TF Example Glossary - Cohort"
  description = "A group of customers sharing a common characteristic or acquisition period"
  parent_node = datahub_glossary_node.segmentation.urn
}

# Sibling of Revenue, added so the isA relationship below has a genuine
# specialisation to point at.
resource "datahub_glossary_term" "recurring_revenue" {
  term_id     = "tf-example-glossary-recurring-revenue"
  name        = "TF Example Glossary - Recurring Revenue"
  description = "Revenue expected to repeat in future periods, such as subscriptions"
  parent_node = datahub_glossary_node.finance.urn
}

# ---------------------------------------------------------------------------
# Term relationships -- typed edges between glossary terms
#
# Each edge is its own resource; relationships added outside Terraform on the
# same terms are left untouched. Both term references use .urn expressions so
# Terraform creates the terms before the edges and destroys the edges before
# the terms. DataHub validates both ends at write time, so a raw URN string
# pointing at a not-yet-created term would fail the apply.
# ---------------------------------------------------------------------------

# Recurring Revenue is a kind of Revenue. In the DataHub UI this shows as
# "Inherits" on Recurring Revenue and "Inherited By" on Revenue.
resource "datahub_glossary_term_relationship" "recurring_revenue_is_a_revenue" {
  term_urn          = datahub_glossary_term.recurring_revenue.urn
  relationship_type = "isA"
  related_term_urn  = datahub_glossary_term.revenue.urn
}

# Churn is one of the metrics reported for each cohort, so the Cohort concept
# contains it. In the DataHub UI this shows as "Contains" on Cohort and
# "Contained By" on Churn.
resource "datahub_glossary_term_relationship" "cohort_has_a_churn" {
  term_urn          = datahub_glossary_term.cohort.urn
  relationship_type = "hasA"
  related_term_urn  = datahub_glossary_term.churn.urn
}
