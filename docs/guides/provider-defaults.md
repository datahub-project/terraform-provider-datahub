---
page_title: "Provider-level defaults: labels for Terraform-managed entities"
subcategory: ""
description: |-
  Automatically attach custom properties, tags, and structured properties to every resource the provider manages - including the managed-by marker that is on by default.
---

# Provider-level defaults

The provider can automatically attach labels to every resource it manages, in the spirit of the AWS provider's `default_tags` and the Google provider's `default_labels`. Typical uses: a provenance marker (`managed-by = "terraform"`), distinguishing the estates of multiple Terraform stacks sharing one DataHub instance, and search-filterable ownership markers.

```terraform
provider "datahub" {
  # On by default (no configuration needed):
  # auto_properties        = ["managed-by"]     # writes managed-by = "terraform"
  # auto_property_strategy = "CREATION_ONLY"

  defaults = {
    custom_properties = { team = "data-platform" }
    tags              = ["urn:li:tag:terraform-managed"]
    structured_properties = {
      "urn:li:structuredProperty:io.example.stack" = ["prod"]
    }
  }
}
```

## What applies where

DataHub entity types support different label aspects, so coverage varies by resource:

| Resource | Custom properties | Structured properties | Tags |
|---|---|---|---|
| `datahub_domain`, `datahub_glossary_term`, `datahub_glossary_node` | yes | yes | no |
| `datahub_corp_user`, `datahub_service_account` | yes | yes | yes |
| `datahub_corp_group` | no | yes | yes |
| `datahub_data_product` | yes | yes | yes |
| `datahub_data_contract` | no | yes | no |
| assertion resources (`datahub_custom_assertion`, `datahub_field_assertion`, ...) | no | no | yes |
| everything else (`datahub_ingestion_source`, `datahub_secret`, `datahub_policy`, `datahub_connection`, ...) | none - the DataHub entity model does not register label aspects on these types today, so defaults are a documented no-op there |

A resource that supports several mechanisms receives **all** the configured ones simultaneously - the mechanisms are independent, not a fallback chain.

## The managed-by marker (`auto_properties`)

The `managed-by = "terraform"` custom property is written automatically to every custom-property-capable resource - it is the only part of the feature that is on by default, because it needs no setup. An optional second marker, `provider-version`, records the provider version. Disable with `auto_properties = []`; removing a marker name removes the property from all managed entities on the next apply.

`auto_property_strategy` controls stamping:

- `CREATION_ONLY` (default): markers are added only when an entity is **created**, and their values are frozen at creation. Upgrading the provider never produces diffs on an existing estate, and a `provider-version` stamp acts as a birth certificate that never drifts. The trade-off: entities created before the feature existed carry no marker until they are recreated - so marker *absence* does not prove an entity is unmanaged.
- `PROACTIVE`: markers and their live values are enforced on every managed entity on every apply. Run it once to converge an existing estate (then optionally switch back), or leave it on to keep `provider-version` current at the cost of a diff wave after each provider upgrade.

## Precedence and collisions

For custom properties, the merge order is: **auto-property markers < `defaults.custom_properties` < the resource's own `custom_properties`** - most specific wins per key. Overriding a marker in `defaults.custom_properties` is silent (it is the supported way to change a marker's value). A resource-level key that overrides a provider default with a *different* value raises a plan-time warning on every plan while the conflict exists; identical values are silent and produce no diff.

Each affected resource exposes the effective result as computed attributes: `custom_properties_all` (the complete written map), `tags_all` (the complete tag list while owned), and `structured_properties_defaults` (only the default-managed properties - deliberately not the full server view).

## Ownership: what apply will overwrite

- **Custom properties**: the provider owns the complete map on managed entities. Properties added outside Terraform are removed on the next apply.
- **Tags**: ownership is guarded by a latch. With no `defaults.tags` configured the provider neither reads nor writes an entity's tags - UI-applied tags are invisible to Terraform and never touched. Once `defaults.tags` is set, managed entities are latched: the provider owns the complete tag list, and externally added tags show as drift on `tags_all` and are removed on apply. Removing `defaults.tags` clears the provider's tags and releases the latch.
- **Structured properties**: ownership is **per property URN**. Only the properties named in `defaults.structured_properties` are managed; properties assigned via `datahub_structured_property_assignment` resources or outside Terraform are neither shown nor touched. Do not manage the same property URN through both the defaults and an assignment resource on the same entity - the provider warns if you do.

Structured property defaults are applied only to resources whose entity type appears in the property definition's `entity_types`; other resources skip that property silently.

## Bootstrap ordering

Provider configuration cannot depend on resources created in the same apply. Tags referenced in `defaults.tags` and definitions referenced in `defaults.structured_properties` must already exist - create them in a prior apply (or a bootstrap stack). Referencing a missing tag fails at apply time with a clear error; a missing property definition produces a warning and the property is skipped until it exists.

## Destroy ordering

When destroying a configuration that contains both a marker tag (or property definition) and entities still carrying it, remove the defaults first:

1. Apply once with the `defaults.tags` / `defaults.structured_properties` entry removed (this clears the values from managed entities), then
2. Destroy.

Destroying the tag or definition in the same operation as still-labelled entities races DataHub's asynchronous reference-cleanup, which can leave behind invisible "husk" entities that block later re-creation with "already exists" errors. If that happens, remove the husk with `datahub delete --hard --urn <urn>` once the cleanup has settled, and recreate.

## A search-filterable marker

Custom properties are free-text indexed but not a search facet. For a marker you can filter on in DataHub search, use a structured property:

```terraform
# Apply 1: the definition
resource "datahub_structured_property" "managed_by" {
  property_id  = "io.example.terraform.managedBy"
  display_name = "Managed by Terraform"
  value_type   = "string"
  entity_types = [
    "domain", "glossaryTerm", "glossaryNode",
    "corpuser", "corpGroup", "dataProduct", "dataContract",
  ]
}
```

```terraform
# Apply 2 onward: the marker
provider "datahub" {
  defaults = {
    structured_properties = {
      "urn:li:structuredProperty:io.example.terraform.managedBy" = ["true"]
    }
  }
}
```

## Opting out per workspace or module

There is no per-resource opt-out attribute. To manage some resources without defaults, use a provider alias:

```terraform
provider "datahub" {
  alias = "no_defaults"
  defaults = {}
  auto_properties = []
}

resource "datahub_domain" "unlabelled" {
  provider  = datahub.no_defaults
  domain_id = "scratch"
  name      = "Scratch"
}
```
