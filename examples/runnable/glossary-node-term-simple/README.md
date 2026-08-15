# glossary-node-term-simple

Creates a two-level Business Glossary hierarchy in DataHub -- two root term groups, each with one child term group and glossary terms at different depths -- and then wires two typed relationships between the terms.

```
TF Example Glossary - Finance                 (root term group)
  |- TF Example Glossary - Revenue            (term, direct child)
  |- TF Example Glossary - Recurring Revenue  (term, direct child)
  +- TF Example Glossary - Accounting         (child term group)
       +- TF Example Glossary - Accrual       (term)

TF Example Glossary - Customer                (root term group)
  |- TF Example Glossary - Churn              (term, direct child)
  +- TF Example Glossary - Segmentation       (child term group)
       +- TF Example Glossary - Cohort        (term)

Relationships:
  Recurring Revenue --isA-->  Revenue          (Recurring Revenue inherits Revenue)
  Cohort            --hasA--> Churn            (Cohort contains Churn)
```

This example illustrates:
- Root-level and nested term groups (`datahub_glossary_node`)
- Terms (`datahub_glossary_term`) attached at two different depths
- Typed term-to-term relationships (`datahub_glossary_term_relationship`), one per edge, covering both relationship types: `isA` (specialisation) and `hasA` (composition)
- Using `.urn` references to express parent-child relationships and relationship endpoints -- required for Terraform to order creates and destroys correctly

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

The Business Glossary page will show the **TF Example Glossary - Finance** and **TF Example Glossary - Customer** term groups. Expand each to see the nested structure.

## Term relationships

The example creates one edge of each relationship type DataHub supports:

- `isA` -- **TF Example Glossary - Recurring Revenue** inherits from **TF Example Glossary - Revenue**. Recurring revenue is a kind of revenue.
- `hasA` -- **TF Example Glossary - Cohort** contains **TF Example Glossary - Churn**. Churn is one of the metrics reported for each cohort.

Each edge is its own `datahub_glossary_term_relationship` resource, and the edge is stored one-sided on the source term (`term_urn`). The stored direction reads in the UI as **Inherits** (`isA`) and **Contains** (`hasA`); the other term shows the reverse lookup as **Inherited By** / **Contained By**.

To see them: open a term's page in the Business Glossary and select its **Related Terms** tab. On *Recurring Revenue* you will find *Revenue* under **Inherits**; on *Revenue*, *Recurring Revenue* under **Inherited By**. On *Cohort* you will find *Churn* under **Contains**; on *Churn*, *Cohort* under **Contained By**.

The composite ids of both edges (`<term_urn>|<relationship_type>|<related_term_urn>`) are exposed as outputs:

```bash
terraform output recurring_revenue_is_a_revenue_id
terraform output cohort_has_a_churn_id
```

Relationships added to the same terms outside Terraform are left untouched: each resource owns only its single edge.

## Destroy ordering

DataHub's `deleteGlossaryEntity` mutation does not check for children before deleting -- it succeeds even if a term group still has terms or sub-groups. This means the server provides no safety net for out-of-order deletes.

In this example every `parent_node`, `term_urn` and `related_term_urn` is set to `<resource>.urn` (a Terraform reference, not a raw URN string). Terraform uses these edges to build a dependency graph, which guarantees that relationships are destroyed before the terms they connect, terms before their parent nodes, and child nodes before root nodes. Never replace `.urn` references with hard-coded URN strings, as doing so removes those edges -- and for relationships it also matters on create: DataHub validates that both terms exist at write time, so an edge referencing a not-yet-created term fails the apply.

## The automatic managed-by marker

When you apply this example, a `managed-by = "terraform"` custom property will appear on every node and term. That is the provider's `auto_properties` marker, which is on by default for every custom-property-capable resource -- see the "Provider-level defaults" guide. To apply the example without it, add `auto_properties = []` to the provider block.

## Cleanup

```bash
terraform destroy
```
