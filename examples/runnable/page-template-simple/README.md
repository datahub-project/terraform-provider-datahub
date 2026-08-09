# Page template (simple)

The minimal use of `datahub_page_module` and `datahub_page_template`: two modules, one template, two rows.

This example deliberately does **not** touch the page your users see. It creates a template under its own id, which nothing points at. For changing the organisation's home page — and the extra care that needs — see `home-page-layout`.

## Prerequisites

- A reachable DataHub instance, OSS or Cloud.
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` exported.
- A token whose principal holds **Manage Home Page Templates**. DataHub rejects any write to a `GLOBAL`-scoped template or module without it.
- `curl` and `jq`, used by the commands this example emits as outputs.

## Run

```bash
export DATAHUB_GMS_URL="http://localhost:8080"
export DATAHUB_GMS_TOKEN="<your-token>"

terraform init
terraform apply
```

## Verify

Read back what DataHub stored:

```bash
eval "$(terraform output -raw verify_command)"
```

Nothing changes in the UI, because no page points at this template. To actually look at it, switch your own home page to it:

```bash
eval "$(terraform output -raw view_as_yourself_command)"
```

Then reload the DataHub home page. To go back to the organisation default:

```bash
eval "$(terraform output -raw reset_yourself_command)"
```

Both of those affect only you, and need no privilege — the pointer is hard-scoped to the calling user, so nobody can change anyone else's page.

## Cleanup

```bash
terraform destroy
```

Terraform created this template, so destroy deletes it. (A template that already existed and was *adopted* is restored to its previous layout instead — that is the `home-page-layout` case.)

Run `reset_yourself_command` first if you pointed yourself at the template, or your home page will reference something that no longer exists.

## Notes

- **Only four module types can be created**: `RICH_TEXT`, `LINK`, `ASSET_COLLECTION` and `HIERARCHY`, each requiring its matching `params` block. Types such as `DOMAINS`, `DATA_PRODUCTS`, `OWNED_ASSETS` and `PLATFORMS` are modules DataHub bootstraps per instance — reference their URNs in a row, as the second row here does with `top_domains`.
- **Modules are separate resources, not blocks.** DataHub addresses them independently and one module can appear on several templates. Referencing `datahub_page_module.x.urn` from `rows` is what gives Terraform the ordering, so no `depends_on` is needed.
- **The template owns the whole layout.** Every apply sends the complete set of rows.
- Open-source DataHub ships no home-page editing UI at all, so this provider is the only practical way to build one there.
