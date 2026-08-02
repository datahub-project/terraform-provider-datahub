# domain-simple

Creates a two-level domain hierarchy in DataHub: two root domains, each with two child domains.

```
TF Example Domain - Finance             (root domain)
  +- TF Example Domain - Accounting     (child domain)
  +- TF Example Domain - Treasury       (child domain)

TF Example Domain - Engineering         (root domain)
  +- TF Example Domain - Data Platform  (child domain)
  +- TF Example Domain - Analytics      (child domain)
```

This example illustrates:
- Root domains (no parent)
- Child domains nested under a root via `parent_domain`
- Using `.urn` references to express parent-child relationships -- required for Terraform to order creates and destroys correctly

## Prerequisites

- Terraform CLI 1.11 or later
- A running DataHub instance (OSS or Cloud)
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` set in the shell
- The token must belong to a principal with the `MANAGE_DOMAINS` DataHub privilege

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
echo "$DATAHUB_GMS_URL/domains"
```

## Destroy ordering

DataHub refuses to hard-delete a domain that still has child domains. Terraform handles this correctly because every `parent_domain` in this example is set to `<resource>.urn` (a Terraform reference, not a raw URN string). Those references give Terraform the dependency edges it needs to destroy children before parents.

If you replace a `.urn` reference with a hard-coded URN string, Terraform loses that edge and may attempt to destroy the parent before its children, causing a `terraform destroy` failure.

## The automatic managed-by marker

When you apply this example, a `managed-by = "terraform"` custom property will appear on every domain alongside the resources you declared. That is the provider's `auto_properties` marker, which is on by default for every custom-property-capable resource -- see the "Provider-level defaults" guide. To apply the example without it, add `auto_properties = []` to the provider block.

## Cleanup

```bash
terraform destroy
```
