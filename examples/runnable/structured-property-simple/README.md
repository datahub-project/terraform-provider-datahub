# structured-property-simple

Demonstrates `datahub_structured_property` by creating two typed custom-property definitions:

- **Retention Days** (`tf-example-retention-days`) - a single-valued `number` property applicable to `dataset` entities, shown in search filters and the asset summary panel.
- **Data Classification** (`tf-example-classification`) - a single-valued `string` property with three allowed values (`Public`, `Internal`, `Confidential`), applicable to `dataset` and `dashboard` entities.

Both properties are managed here as *definitions*. Applying values to individual datasets or dashboards is per-asset enrichment and is not in scope for this provider.

## Prerequisites

- DataHub GMS URL and a personal access token with the **Manage Structured Properties** privilege.
- Terraform >= 1.11.

## Usage

```bash
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars and fill in your DataHub URL and token.

terraform init
terraform apply
```

After apply you can verify the properties in the DataHub UI at the URL shown in the `verify_url` output, or at `<your-datahub>/structured-properties`.

## Outputs

| Output | Description |
|---|---|
| `retention_days_urn` | URN of the retention-days property |
| `classification_urn` | URN of the classification property |
| `retention_lookup_display_name` | Display name read back via the data source |
| `all_structured_property_urns` | All property URNs in DataHub (eventually consistent) |
| `verify_url` | Direct link to the structured-properties page in DataHub |

## Cleanup

```bash
terraform destroy
```

This hard-deletes both properties from DataHub and asynchronously removes any values applied to assets.
