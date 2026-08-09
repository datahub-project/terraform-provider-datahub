# Home-page layout

Builds a DataHub home page from Terraform: four modules (rich text, domains, data products, a link) arranged into three rows by a page template.

The point of doing this in Terraform is that the layout becomes part of the estate. A demo or test instance that gets torn down and rebuilt comes back with its landing page intact, because both the modules and the template use fixed, deterministic URNs rather than the random UUIDs DataHub mints when a template is created through the UI.

## This example changes your home page

Applying replaces the page every user sees, except those who have personalised their own. That is the point of it, and it is reversible: `terraform destroy` restores the previous layout rather than deleting the page.

DataHub seeds one default template per instance and provides no API to point at a different one, so changing the home page means editing that template — exactly what the DataHub UI's "edit default template" flow does. The id comes from the `datahub_home_page_settings` data source rather than being hardcoded, since `home_default_1` is a bootstrap value rather than an API guarantee.

**Check which instance you are pointed at first** — `echo $DATAHUB_GMS_URL`. On a shared instance this is a change your colleagues will notice.

For a template that changes nothing users see, use the `page-template-simple` example instead.

## Showing who the default actually reaches

DataHub renders a user's own template if they have one, and the organisation default otherwise. So adopting the default changes the page for everyone **except** people who have personalised theirs — and the only way to see that is with a second person.

Two pieces demonstrate it, and both are created unconditionally — without them this example would be little more than `page-template-simple`.

**A second `GLOBAL` template.** Nothing points at it, and **DataHub has no query that lists templates**, so it is unreachable until somebody is handed its URN. That is why `alternate_template_urn` is an output: for this pattern the output *is* the discovery mechanism, not a convenience.

**⚠️ A second user.** This example **creates a real login** on whatever instance you apply it to — `tf-example-homepage-viewer@example.invalid` by default, overridable with `test_user_email`. They configure nothing, so they see the organisation default, which is the proof that adopting it reached somebody other than the account that ran the apply.

**You supply the password**, and it is never stored:

```bash
export TF_VAR_test_user_password=$(openssl rand -base64 24)
echo "$TF_VAR_test_user_password"    # note it down; you need it to log in
```

`initial_password` is a write-only attribute, so Terraform passes it to DataHub and keeps nothing — not in state, not in the plan file. The plan shows it as `(write-only attribute)` rather than a value.

The example will not invent one for you, and fails at plan time with instructions if you forget. That is deliberate: Terraform cannot generate a secret and show it to you without also keeping it forever. A root output is persisted in state by definition, and an ephemeral value cannot be a root output at all — `Ephemeral outputs are not allowed in context of a root module`. So every generate-and-display scheme, with or without a third-party provider, ends with the credential in state. Supplying it out of band is the only shape that does not.

Keep the variable exported for `terraform destroy` as well; Terraform evaluates variable validation on both.

Once logged in, they can switch themselves to the alternate with two API calls and **no privilege at all**: `updateUserHomePageSettings` is ungated and hard-scoped to the caller, so nobody can change anyone else's page. No access token is needed — a session cookie from `/logIn` is enough.

```bash
terraform output -raw switch_to_alternate_instructions
```

`terraform destroy` removes the user along with everything else. On open-source DataHub the sign-up guard rejects any address whose entity already exists, so a skipped destroy leaves that address unusable on that instance until the user is deleted.

Note there is no way to make the alternate the *organisation* default. Nothing can move that pointer; changing what everyone sees means editing the template it already names.

## Prerequisites

- A reachable DataHub instance, OSS or Cloud.
- `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` exported.
- A token whose principal holds **Manage Home Page Templates**. Without it, DataHub rejects any write to a `GLOBAL`-scoped template or module.

## Run

```bash
export DATAHUB_GMS_URL="http://localhost:8080"
export DATAHUB_GMS_TOKEN="<your-token>"

# The test user's password. Write-only -- never written to state.
export TF_VAR_test_user_password=$(openssl rand -base64 24)
echo "$TF_VAR_test_user_password"

terraform init
terraform apply
```

Passing it as an environment variable rather than `-var` keeps it out of shell history. Forgetting it fails at plan time with instructions rather than silently doing something else.


## Verify

`is_the_live_home_page` tells you whether what you just applied is what users see:

```bash
terraform output is_the_live_home_page
```

Read back the rows DataHub actually stored, through the strongly-consistent path:

```bash
eval "$(terraform output -raw verify_command)"
```

Then open your DataHub instance in a browser and hard-refresh — the front end caches the template, so a plain reload can still show the old layout. Expect three rows: the welcome text, then Domains and Data products side by side, then the runbook link.

## Cleanup

```bash
terraform destroy
```

**That does not delete your home page.** The template already existed -- DataHub bootstraps it -- so destroying restores the layout it had before Terraform adopted it, rather than removing the page every user sees. That layout is captured on the first apply, shown as `original_rows` in `terraform show`, and copied into DataHub as `urn:li:dataHubPageTemplate:tfprovider-backup-home_default_1` so it survives losing your state file.

If instead you want to stop managing the page and *keep* the layout this example applied, remove it from state rather than destroying it:

```bash
terraform state rm datahub_page_template.home
```

## Notes

- **Modules are separate resources, not blocks.** DataHub addresses them independently and one module can appear on several templates. Referencing `datahub_page_module.x.urn` from the template's `rows` is what gives Terraform the ordering, so no `depends_on` is needed.
- **The template owns the whole layout.** Every apply sends the complete set of rows, so a module added to this page in the UI is removed on the next apply.
- **`type` is not validated by the provider.** DataHub's module catalogue grew from 22 types to 30 in a single release, so the provider passes the value through and lets the server reject an unknown one. Types taking no parameters include `DOMAINS`, `DATA_PRODUCTS`, `OWNED_ASSETS`, `ASSETS` and `PLATFORMS`.
- **`PERSONAL` scope is rejected.** Per-user home pages are out of scope for this provider, and a `PERSONAL` template written here would belong to whichever account the token authenticates as.
