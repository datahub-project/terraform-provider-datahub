# org-branding-simple

The minimal use of `datahub_organization_display_preferences`: sets the organization name, logo and brand colour that DataHub shows every user, and reads the previous branding back through the matching data source so you can put it the way it was.

## Read this before applying

This example is not like the others in `examples/runnable/`. The rest create entities under `tf-example-` ids that nobody else is looking at. This one changes a **singleton** — one global settings object per DataHub instance — and the change is visible to **every user of the instance immediately**.

Three specific consequences, each of which has to be planned for rather than discovered:

- **Applying replaces the existing branding for everyone.** Not only the attributes you care about: the provider sends `org_name` and `logo_url` on every write whatever your configuration says, so a configuration that named only `primary_color` would blank the organization name and the logo. That is why this example declares all three.
- **`terraform destroy` resets the branding to DataHub's defaults. It does not restore what was there before.** There is no undo, and DataHub keeps no history of the previous values.
- **Capture the previous branding before you apply.** The `previous_branding` and `restore_snippet` outputs do this for you, but only on the *first* apply — see [Capture the previous branding](#capture-the-previous-branding) below, because the window closes on the next `terraform plan`.

Do not run this against a shared instance you do not administer. A local or personal DataHub Cloud instance is the right target.

## Prerequisites

- Terraform CLI 1.11 or later.
- **A DataHub Cloud instance.** `datahub_organization_display_preferences` is Cloud-only: OSS DataHub exposes no `globalSettings` GraphQL read surface and no `updateOrganizationDisplayPreferences` mutation, so the apply fails with a message saying so rather than doing something partial.
- **DataHub Cloud v2.2.0 or later**, for `primary_color`. On an older Cloud instance the apply fails with a diagnostic naming the attribute; removing the `primary_color` line from `main.tf` manages the remaining two attributes as before.
- A personal access token whose principal holds **Manage Organization Display Preferences** (`MANAGE_ORGANIZATION_DISPLAY_PREFERENCES`).

```bash
export DATAHUB_GMS_URL=https://your-instance.acryl.io/gms
export DATAHUB_GMS_TOKEN=<personal-access-token>
```

## Run

```bash
terraform init
terraform apply
```

`primary_color` has **no default**, so Terraform prompts for it. That is the point: the value recolours the UI for every user, so the example will not choose one on your behalf. Supply it non-interactively if you prefer:

```bash
terraform apply -var "primary_color=#EC0016"
```

`org_name` and `logo_url` do have defaults (`TF Example Branding` and a non-resolving `example.com` URL, so an unmodified apply is obviously a test rather than quietly plausible branding). Override them when adopting this against a real instance:

```bash
terraform apply \
  -var "org_name=Acme Corporation" \
  -var "logo_url=https://cdn.acme.example/logo.png" \
  -var "primary_color=#EC0016"
```

`terraform destroy` asks for `primary_color` as well, because Terraform evaluates the configuration when destroying. **The value is ignored there** — the branding is reset to DataHub's defaults whatever you type, so any valid colour gets you past the prompt.

## Capture the previous branding

The `datahub_organization_display_preferences` data source in `main.tf` has no dependency on the resource, so Terraform reads it during the **plan** phase, before anything is written. On the first `terraform apply` its outputs therefore describe the instance as it was *before* this example touched it.

That is the only chance to record them. The next `terraform plan` or `terraform refresh` re-reads the data source and the outputs become this example's own values.

```bash
terraform output previous_branding
terraform output -raw restore_snippet > branding-restore.tf   # keep this file safe
```

`restore_snippet` is a ready-to-paste `datahub_organization_display_preferences` block carrying the old values. An empty string in it is meaningful rather than filler: DataHub cannot remove one of these fields, only blank it, so `""` is how you ask for the default back.

To restore from this directory without editing anything:

```bash
eval "$(terraform output -raw restore_command)"
```

That works when the instance had a brand colour set. When it did not, `restore_command` says so and points you at `restore_snippet` instead, because this example's `primary_color` variable deliberately refuses the empty string.

## Verify

```bash
terraform output applied_branding
terraform output settings_urn      # always urn:li:globalSettings:0 - it is a singleton
```

In the DataHub Cloud UI:

- **Settings → Preferences → Appearance**, **Branding** section — the organization name, logo and brand colour appear as the configured values. (**Preferences** is a group heading; **Appearance** is the page under it.) Direct link: `$DATAHUB_GMS_URL/settings/preferences`, replacing the `/gms` suffix with the front-end host if your Cloud instance separates them.
- The **navigation bar and browser tab title** pick up the organization name, and the logo replaces the DataHub mark.
- The brand colour is not one element: DataHub derives the UI's brand tokens from it, so buttons, links and highlights all move together. Reload the page — the theme is read at load.

The language selector on the same settings page is a **per-user** preference, not an org-wide one, and is deliberately not managed by this provider.

## Notes

- **`primary_color` is the one attribute this resource does not own until you set it once.** Until then the provider leaves it out of the write entirely, so a colour set in the DataHub UI survives an apply that says nothing about it, and configurations running against a Cloud instance that predates the field keep working. Naming it — which this example does — takes ownership from that moment: remove the line later, or destroy the resource, and it is reset like everything else here.
- **It is a singleton, so manage it from one place.** A second instance of this resource in the same configuration, or the same settings managed from two workspaces, will fight over the values on alternating applies.
- **Adopting existing branding instead of replacing it** is the other way in. `terraform import` takes any id (the URN, or a placeholder such as `-`) because the URN is fixed:

  ```bash
  terraform import datahub_organization_display_preferences.branding urn:li:globalSettings:0
  ```

  Import adopts whatever the instance holds, including the brand colour, so the colour is owned from that moment on. Run `terraform plan` immediately afterwards and reconcile the configuration with what the plan reports before applying anything.

## Cleanup

```bash
terraform destroy -var "primary_color=#EC0016"
```

The colour is required at the prompt but **ignored on destroy** -- any valid six-digit hex gets you through, and the branding is reset regardless of what you type. Passing `-var` avoids the prompt entirely.

**This resets the organization name, logo and brand colour to DataHub's defaults.** It does not restore the branding that was there before this example ran, and it does not delete the global settings object — that is platform state DataHub always expects to exist, and its other sections (SSO, notifications, integrations, the default home page template) are not this resource's to remove.

If you want the previous branding back, apply `restore_snippet` (or run `restore_command`) instead of destroying. Destroying first and restoring afterwards also works, provided you captured the values on the first apply.
