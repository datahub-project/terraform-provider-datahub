# tag-simple

Demonstrates the `datahub_tag` resource, `datahub_tag` data source, and `datahub_tags` plural data source.

Creates three tags with display colours -- PII, Verified, and Deprecated -- reads one back via the singular data source, and enumerates all tags via the plural data source.

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

After apply, navigate to **Govern → Tags** in the DataHub UI. You should see three tags: **TF Example - PII** (red), **TF Example - Verified** (green), and **TF Example - Deprecated** (grey).

## Bulk import pattern

Use `datahub_tags` to import pre-existing tags into Terraform state without recreating them:

```hcl
data "datahub_tags" "existing" {}

import {
  for_each = toset(data.datahub_tags.existing.urns)
  id       = each.value
  to       = datahub_tag.imported[each.value]
}

resource "datahub_tag" "imported" {
  for_each = toset(data.datahub_tags.existing.urns)
  tag_id   = trimprefix(each.value, "urn:li:tag:")
  name     = "placeholder"  # replaced on first plan+apply after import
}
```

## Cleanup

```bash
terraform destroy
```

This hard-deletes all three tags from DataHub.
