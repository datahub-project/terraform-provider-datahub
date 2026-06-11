terraform {
  required_version = ">= 1.11"
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.9.0"
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
  node_id     = "tf-example-finance"
  name        = "TF Example - Finance"
  description = "Financial metrics, accounting concepts, and revenue definitions"
}

resource "datahub_glossary_node" "customer" {
  node_id     = "tf-example-customer"
  name        = "TF Example - Customer"
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
  node_id     = "tf-example-accounting"
  name        = "TF Example - Accounting"
  description = "Accounting standards and recognition principles"
  parent_node = datahub_glossary_node.finance.urn
}

resource "datahub_glossary_node" "segmentation" {
  node_id     = "tf-example-segmentation"
  name        = "TF Example - Segmentation"
  description = "Customer segmentation and cohort analysis concepts"
  parent_node = datahub_glossary_node.customer.urn
}

# ---------------------------------------------------------------------------
# Glossary terms -- leaf nodes attached to their parent term group
#
# Two terms hang directly off root nodes (demonstrating shallow terms); two
# hang off second-level nodes (demonstrating deeper nesting). All four use
# .urn references so Terraform destroys terms before their parent nodes.
# ---------------------------------------------------------------------------

# Direct child of the Finance root node.
resource "datahub_glossary_term" "revenue" {
  term_id     = "tf-example-revenue"
  name        = "TF Example Revenue"
  description = "Total revenue recognised in the reporting period before any deductions"
  parent_node = datahub_glossary_node.finance.urn
}

# Child of the Accounting sub-node (depth 2).
resource "datahub_glossary_term" "accrual" {
  term_id     = "tf-example-accrual"
  name        = "TF Example Accrual"
  description = "Revenue or expense recorded when earned or incurred, regardless of cash movement"
  parent_node = datahub_glossary_node.accounting.urn
}

# Direct child of the Customer root node.
resource "datahub_glossary_term" "churn" {
  term_id     = "tf-example-churn"
  name        = "TF Example Churn"
  description = "Rate at which customers discontinue their subscription or stop purchasing"
  parent_node = datahub_glossary_node.customer.urn
}

# Child of the Segmentation sub-node (depth 2).
resource "datahub_glossary_term" "cohort" {
  term_id     = "tf-example-cohort"
  name        = "TF Example Cohort"
  description = "A group of customers sharing a common characteristic or acquisition period"
  parent_node = datahub_glossary_node.segmentation.urn
}
