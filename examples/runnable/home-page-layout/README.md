# Home-page layout

Builds a DataHub home page from Terraform: four modules (rich text, domains, data products, a link) arranged into three rows by a page template.

The point of doing this in Terraform is that the layout becomes part of the estate. A demo or test instance that gets torn down and rebuilt comes back with its landing page intact, because both the modules and the template use fixed, deterministic URNs rather than the random UUIDs DataHub mints when a template is created through the UI.

## Two modes, and the difference matters

**Default (`adopt_default_template = false`): safe, and invisible.** Creates its own template, `tf-example-homepage-demo`. It applies and destroys cleanly and changes nothing that users see, because nothing points at it. This is the mode to run first, and the mode to run on a shared instance.

**`adopt_default_template = true`: this replaces your organisation's home page.** DataHub seeds one default template per instance and provides no API to point at a different one, so changing the home page means editing that template -- exactly what the DataHub UI's "edit default template" flow does. The example reads which template that is from the `datahub_home_page_settings` data source rather than hardcoding `home_default_1`, since the id is a bootstrap value rather than an API guarantee.

Read the cleanup section before switching it on.

## Showing who the default actually reaches

DataHub renders a user's own template if they have one, and the organisation default otherwise. So adopting the default changes the page for everyone **except** people who have personalised theirs — and the only way to see that is with a second person.

Two optional pieces demonstrate it:

- **`create_alternate_template`** (on by default) publishes a second `GLOBAL` template. Nothing points at it, and **DataHub has no query that lists templates**, so it is unreachable until somebody is handed its URN. That is why `alternate_template_urn` is an output: for this pattern the output *is* the discovery mechanism, not a convenience.
- **`create_test_user`** (off by default) creates a user who has configured nothing. Log in as them and you see the organisation default — which is the proof that adopting it reached somebody other than the account that ran `terraform apply`.

Any user can then switch themselves to the alternate with two API calls and **no privilege at all**: `updateUserHomePageSettings` is ungated and hard-scoped to the caller, so nobody can change anyone else's page. `terraform output switch_to_alternate_instructions` prints them. No personal access token is needed — a session cookie from `/logIn` is enough.

```bash
terraform apply -var create_test_user=true -var test_user_password='...'
terraform output -raw switch_to_alternate_instructions
```

Note there is no way to make the alternate the *organisation* default. Nothing can move that pointer; changing what everyone sees means editing the template it already names.

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

```bash
terraform destroy
```

That works in both modes, with a difference worth knowing.

In default mode it deletes the demonstration template it created, because Terraform created it.

**With `adopt_default_template = true` it does not delete your home page.** The template already existed -- DataHub bootstraps it -- so destroying restores the layout it had before Terraform adopted it, rather than removing the page every user sees. That layout is captured on the first apply, shown as `original_rows` in `terraform show`, and copied into DataHub as `urn:li:dataHubPageTemplate:tfprovider-backup-home_default_1` so it survives losing your state file.

If instead you want to stop managing the page and *keep* the layout this example applied, remove it from state rather than destroying it:

```bash
terraform state rm datahub_page_template.home
```

## Notes

- **Modules are separate resources, not blocks.** DataHub addresses them independently and one module can appear on several templates. Referencing `datahub_page_module.x.urn` from the template's `rows` is what gives Terraform the ordering, so no `depends_on` is needed.
- **The template owns the whole layout.** Every apply sends the complete set of rows, so a module added to this page in the UI is removed on the next apply.
- **`type` is not validated by the provider.** DataHub's module catalogue grew from 22 types to 30 in a single release, so the provider passes the value through and lets the server reject an unknown one. Types taking no parameters include `DOMAINS`, `DATA_PRODUCTS`, `OWNED_ASSETS`, `ASSETS` and `PLATFORMS`.
- **`PERSONAL` scope is rejected.** Per-user home pages are out of scope for this provider, and a `PERSONAL` template written here would belong to whichever account the token authenticates as.
