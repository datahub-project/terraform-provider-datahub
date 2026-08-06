# Home-page layout

Builds a DataHub home page from Terraform: four modules (rich text, domains, data products, a link) arranged into three rows by a page template.

The point of doing this in Terraform is that the layout becomes part of the estate. A demo or test instance that gets torn down and rebuilt comes back with its landing page intact, because both the modules and the template use fixed, deterministic URNs rather than the random UUIDs DataHub mints when a template is created through the UI.

## Two modes, and the difference matters

**Default (`adopt_default_template = false`): safe, and invisible.** Creates its own template, `tf-example-homepage-demo`. It applies and destroys cleanly and changes nothing that users see, because nothing points at it. This is the mode to run first, and the mode to run on a shared instance.

**`adopt_default_template = true`: this replaces your organisation's home page.** DataHub seeds one default template per instance and provides no API to point at a different one, so changing the home page means editing that template -- exactly what the DataHub UI's "edit default template" flow does. The example reads which template that is from the `datahub_home_page_settings` data source rather than hardcoding `home_default_1`, since the id is a bootstrap value rather than an API guarantee.

Read the cleanup section before switching it on.

## Prerequisites

- A reachable DataHub instance, OSS or Cloud.
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` exported.
- A token whose principal holds **Manage Home Page Templates**. Without it, DataHub rejects any write to a `GLOBAL`-scoped template or module.

## Run

```bash
export DATAHUB_GMS_URL="http://localhost:8080"
export DATAHUB_GMS_TOKEN="<your-token>"

terraform init
terraform apply
```

To manage the real home page:

```bash
terraform apply -var adopt_default_template=true
```

## Verify

`is_the_live_home_page` tells you whether what you just applied is what users see:

```bash
terraform output is_the_live_home_page
```

Read back the rows DataHub actually stored, through the strongly-consistent path:

```bash
eval "$(terraform output -raw verify_command)"
```

In the UI, open `$DATAHUB_GMS_URL/`. In default mode the home page is unchanged -- that is expected, and `is_the_live_home_page` will be `false`.

## Cleanup

In default mode:

```bash
terraform destroy
```

**With `adopt_default_template = true`, `terraform destroy` is refused, deliberately.** Deleting the template the instance points at would leave the organisation with no home page and a dangling pointer, and nothing can aim it elsewhere. The provider raises an error rather than doing it.

To hand the layout back without destroying it:

```bash
terraform state rm datahub_page_template.home
terraform destroy   # removes the modules only
```

The page keeps whatever layout was last applied, and the DataHub UI can edit it again. To restore DataHub's original layout, edit the default template in the UI, or re-apply this example with rows matching the bootstrap default (`your_assets`, then `top_domains` and `platforms`).

## Notes

- **Modules are separate resources, not blocks.** DataHub addresses them independently and one module can appear on several templates. Referencing `datahub_page_module.x.urn` from the template's `rows` is what gives Terraform the ordering, so no `depends_on` is needed.
- **The template owns the whole layout.** Every apply sends the complete set of rows, so a module added to this page in the UI is removed on the next apply.
- **`type` is not validated by the provider.** DataHub's module catalogue grew from 22 types to 30 in a single release, so the provider passes the value through and lets the server reject an unknown one. Types taking no parameters include `DOMAINS`, `DATA_PRODUCTS`, `OWNED_ASSETS`, `ASSETS` and `PLATFORMS`.
- **`PERSONAL` scope is rejected.** Per-user home pages are out of scope for this provider, and a `PERSONAL` template written here would belong to whichever account the token authenticates as.
