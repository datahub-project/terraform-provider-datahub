# data-product-simple

Creates two DataHub data products in a new domain, then reads each one back via the singular data source and enumerates all products via the plural data source.

## What this example does

- Creates a `datahub_domain` (`tf-example-dp-sales`) to act as the owning domain.
- Creates two `datahub_data_product` resources:
  - `tf-example-orders` - with description, external URL, and custom properties
  - `tf-example-customer-360` - minimal (name + domain only)
- Creates a marker `datahub_tag` and `datahub_structured_property`, and (once `enable_defaults = true`) applies them to both data products via the provider's `defaults.tags` and `defaults.structured_properties` -- see "Provider-level defaults" below.
- Reads each product back individually via `datahub_data_product` (strongly consistent, by known ID).
- Enumerates all data products via `datahub_data_products` (bulk URN list, eventually consistent).
- A `managed-by = "terraform"` custom property also appears on both products automatically -- the provider's `auto_properties` marker, on by default for every custom-property-capable resource. To apply the example without it, add `auto_properties = []` to the provider block.

## Prerequisites

- A running DataHub instance (OSS Quickstart or DataHub Cloud).
- A DataHub token with the `MANAGE_DATA_PRODUCTS`, `MANAGE_DOMAINS`, `MANAGE_TAGS`, `MANAGE_STRUCTURED_PROPERTIES`, and `EDIT_ENTITY_TAGS` privileges -- the Admin role has all five.
- Terraform >= 1.11 and the provider binary installed (run `make install` from the repo root).

## Steps

### 1. Configure credentials

```bash
export DATAHUB_GMS_URL="http://localhost:8080"   # or your Cloud instance URL
export DATAHUB_GMS_TOKEN="your-token-here"
```

### 2. Apply

```bash
cd examples/runnable/data-product-simple
terraform init
terraform apply
```

Terraform creates the domain and both data products. After apply, `orders_urn`, `customer_360_urn`, `orders_details`, and `customer_360_details` are printed.

### 3. Verify in the DataHub UI

Navigate to **Govern -> Data Products** in the DataHub UI. You should see `TF Example - Orders` and `TF Example - Customer 360` listed under the `TF Example - Sales` domain.

Or check the `ui_url` output for a direct link:

```bash
terraform output -raw ui_url
```

### 4. Import an existing data product

If a data product already exists in DataHub and you want to bring it under Terraform management:

```bash
terraform import datahub_data_product.existing urn:li:dataProduct:<id>
# or by bare id:
terraform import datahub_data_product.existing <id>
```

To bulk-import all data products in an environment, use `datahub_data_products` to enumerate URNs, then feed them into an `import {}` block. Because Terraform requires `for_each` keys to be known at plan time, this is a two-pass operation:

```bash
# Pass 1: apply just the data source to materialise the URN list.
terraform apply -target=data.datahub_data_products.all

# Pass 2: use the known URNs in an import block.
terraform apply
```

The `import {}` block in your config:

```hcl
import {
  for_each = toset(data.datahub_data_products.all.urns)
  id       = each.value
  to       = datahub_data_product.imported[trimprefix(each.value, "urn:li:dataProduct:")]
}
```

### 5. Turn on provider-level tag and structured-property defaults

The first apply only creates the bootstrap marker tag (`tf-example-dp-managed`) and marker structured property (`io.example.terraform.dpManagedBy`) -- `var.enable_defaults` defaults to `false`, so the provider's `defaults` block is `null` and neither product is touched yet.

Provider configuration is evaluated once, before any resource in the apply runs, so `defaults.tags` / `defaults.structured_properties` cannot reference a tag or property this same apply would create (the "Provider-level defaults" guide's bootstrap ordering rule). Now that they exist, turn defaults on:

```bash
terraform apply -var enable_defaults=true
```

Both `tf-example-orders` and `tf-example-customer-360` pick up the marker tag and the marker structured property, with no changes to the resource blocks themselves -- that is the point of provider-level defaults. Check the result:

```bash
terraform output orders_tags_all
terraform output orders_structured_properties_defaults
terraform output customer_360_tags_all
```

To turn defaults back off (and clear the values from both products) without destroying anything:

```bash
terraform apply -var enable_defaults=false
```

### 6. Asset membership

This example creates the data product definition only. To add datasets, charts, or other assets to a data product, use the DataHub UI (**Govern -> Data Products -> Add Assets**) or the Python SDK:

```bash
datahub dataproduct upsert --urn urn:li:dataProduct:tf-example-orders \
  --asset urn:li:dataset:(urn:li:dataPlatform:postgres,public.orders,PROD)
```

Asset membership managed outside Terraform is not affected by `terraform apply`.

### 7. Destroy

If you turned defaults on (step 5), turn them off first -- this clears the tag and structured-property values from both data products, releasing the ownership latch:

```bash
terraform apply -var enable_defaults=false
```

Destroying the marker tag or structured property in the same operation as data products still carrying them races DataHub's asynchronous reference-cleanup and can leave behind an invisible "husk" entity that blocks later re-creation with an "already exists" error (see the "Provider-level defaults" guide's destroy ordering section). Then:

```bash
terraform destroy
```

This removes both data products, the domain, and the marker tag and structured property from DataHub.
