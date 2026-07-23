# structured-and-custom-properties

Demonstrates the two kinds of properties DataHub supports, side by side, on glossary entities (which render both in the UI): free-form **custom properties** and defined-and-validated **structured properties**, including assigning one structured property across two entity types and the way the UI folds structured properties into a group by their dotted qualified name.

```
TF Example - Governance          (glossary node)
  structured: Regions = [GLOBAL, EMEA]
  +- TF Example Revenue          (glossary term)
       custom:     steward, source_system        (flat)
       structured: Regions = [GLOBAL, APAC], Tier = Gold   (grouped)
```

This example illustrates:

- **Custom properties** (`custom_properties` on `datahub_glossary_term`) - a flat, free-form, Terraform-owned key/value map defined inline on the entity.
- **Structured properties** (`datahub_structured_property`) - defined once with a `value_type`, `cardinality`, and `allowed_values`; validated server-side.
- **Assignment** (`datahub_structured_property_assignment`) - one `(entity, property)` edge per resource. The `Regions` property is defined for both `glossaryTerm` and `glossaryNode` and assigned to one of each, sharing the value `GLOBAL` with one region unique to each. `AMER` is an allowed value left deliberately unassigned.
- **UI grouping via the qualified name** - both structured properties share the `tf-example.governance.` prefix, so DataHub's Properties tab folds them under a single `tf-example.governance` group. The grouping is derived from the dotted name; it is not a separate object you manage. Custom properties render flat, outside that group.

## Prerequisites

- Terraform CLI 1.11 or later
- A running DataHub instance (OSS or Cloud)
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` set in the shell
- A token whose principal can manage the Business Glossary (`MANAGE_GLOSSARIES`), manage structured properties (`MANAGE_STRUCTURED_PROPERTIES`), and edit properties on the target entities (`EDIT_ENTITY_PROPERTIES`) -- the Admin role has all three

## Apply

```bash
export DATAHUB_GMS_URL=https://your-instance.acryl.io/gms
export DATAHUB_GMS_TOKEN=<personal-access-token>

terraform init
terraform apply
```

## Verify

```bash
# Print the created URNs, assignment ids, and a summary
terraform output

# Or open the term in the DataHub UI:
echo "$DATAHUB_GMS_URL/glossary"
```

Open **TF Example Revenue** and select the **Properties** tab. The two structured properties (Regions, Tier) appear nested under a `tf-example.governance` group, while the custom properties (steward, source_system) appear flat. Open **TF Example - Governance** to see the same `Regions` property carrying a different value set on a different entity type.

## Structured vs custom properties

Both attach key/value metadata, but they behave differently:

- **Custom properties** are defined inline on each entity, are not validated, and are not shared or discoverable as a definition. Terraform owns the complete map here -- keys added elsewhere are removed on the next apply.
- **Structured properties** are a first-class entity: defined once, constrained by a value type and (optionally) an allowed-value set, applicable to declared entity types, and assigned to entities as separate edges. They are searchable/filterable and, because their qualified names are dotted, the UI presents them in a nested group.

## The automatic managed-by marker

When you apply this example, a `managed-by = "terraform"` custom property will appear on the glossary node and term alongside the properties the example sets explicitly. That is the provider's `auto_properties` marker, which is on by default for every custom-property-capable resource -- see the "Provider-level defaults" guide. To apply the example without it, add `auto_properties = []` to the provider block.

## A note on ordering

The term receives two structured-property assignments (`Regions` and `Tier`). `tier_term` sets `depends_on = [datahub_structured_property_assignment.regions_term]` so Terraform applies the two writes to that single entity sequentially rather than in parallel. On this provider version concurrent structured-property writes to the same entity can race server-side and lose a value; serialising them avoids it. Assignments to different entities (the node vs the term) run in parallel as usual.

## Cleanup

```bash
terraform destroy
```
