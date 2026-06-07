# ownership-type-simple

Demonstrates the `datahub_ownership_type` resource, `datahub_ownership_type` data source, and `datahub_ownership_types` plural data source.

Creates two custom ownership types -- Data Quality Lead and Data Producer -- and enumerates all ownership types via the plural data source. A `for_each` data source resolves each URN to its full attributes, producing the `ownership_types` map output keyed by URN. Because the list is eventually-consistent (GraphQL-backed), newly created types appear in the map on the next `terraform plan` or `terraform refresh` rather than in the same apply.

## Prerequisites

- Terraform >= 1.11
- A running DataHub instance (OSS or Cloud)
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` environment variables set

## Run

```bash
export DATAHUB_GMS_URL=http://localhost:8080
export DATAHUB_GMS_TOKEN=your-token-here

terraform init
terraform apply
```

## Verify

After apply, `data_quality_lead_urn` and `data_producer_urn` show the URNs of the two created types. `ownership_types` is a map of all types that existed before this apply (system types plus any pre-existing custom ones). Run `terraform refresh` or `terraform plan` again once the GraphQL index has caught up and the two new types will appear in the map too.

Navigate to **Settings → Ownership Types** in the DataHub UI to confirm the two new types appear alongside the built-in ones.

## Bulk import pattern

Use `datahub_ownership_types` to import pre-existing custom ownership types into Terraform state without recreating them:

```hcl
data "datahub_ownership_types" "existing" {}

import {
  for_each = toset([
    for urn in data.datahub_ownership_types.existing.urns
    : urn if !startswith(urn, "urn:li:ownershipType:__system__")
  ])
  id = each.value
  to = datahub_ownership_type.imported[each.value]
}

resource "datahub_ownership_type" "imported" {
  for_each = toset([
    for urn in data.datahub_ownership_types.existing.urns
    : urn if !startswith(urn, "urn:li:ownershipType:__system__")
  ])
  type_id = trimprefix(each.value, "urn:li:ownershipType:")
  name    = "placeholder"  # replaced on first plan+apply after import
}
```

The filter on `__system__` excludes built-in system types, which cannot be managed by Terraform.

## Cleanup

```bash
terraform destroy
```

This hard-deletes the two custom ownership types from DataHub. Built-in system types looked up via the data source are not affected.
