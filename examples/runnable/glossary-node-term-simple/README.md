# glossary-node-term-simple

Creates a two-level Business Glossary hierarchy in DataHub: two root term groups, each with one child term group and two glossary terms at different depths.

```
TF Example - Finance          (root term group)
  |- TF Example Revenue       (term, direct child)
  +- TF Example - Accounting  (child term group)
       +- TF Example Accrual  (term)

TF Example - Customer           (root term group)
  |- TF Example Churn           (term, direct child)
  +- TF Example - Segmentation  (child term group)
       +- TF Example Cohort     (term)
```

This example illustrates:
- Root-level and nested term groups (`datahub_glossary_node`)
- Terms (`datahub_glossary_term`) attached at two different depths
- Using `.urn` references to express parent-child relationships -- required for Terraform to order creates and destroys correctly

## Prerequisites

- Terraform CLI 1.11 or later
- A running DataHub instance (OSS or Cloud)
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` set in the shell
- The token must belong to a principal with permission to manage the Business Glossary (`MANAGE_GLOSSARIES` privilege on OSS; Admin or Metadata Manager role on Cloud)

## Apply

```bash
export DATAHUB_GMS_URL=https://your-instance.acryl.io/gms
export DATAHUB_GMS_TOKEN=<personal-access-token>

terraform init
terraform apply
```

## Verify

```bash
# Print all created URNs
terraform output

# Or check the DataHub UI directly:
echo "$DATAHUB_GMS_URL/glossary"
```

The Business Glossary page will show the Finance and Customer term groups. Expand each to see the nested structure.

## Destroy ordering

DataHub's `deleteGlossaryEntity` mutation does not check for children before deleting -- it succeeds even if a term group still has terms or sub-groups. This means the server provides no safety net for out-of-order deletes.

In this example every `parent_node` is set to `<resource>.urn` (a Terraform reference, not a raw URN string). Terraform uses these edges to build a dependency graph, which guarantees that terms are destroyed before their parent nodes and child nodes before root nodes. Never replace `.urn` references with hard-coded URN strings, as doing so removes those edges.

## Cleanup

```bash
terraform destroy
```
