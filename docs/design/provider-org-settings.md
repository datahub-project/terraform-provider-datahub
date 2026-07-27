# Org-level settings: the small-singleton pattern

Maintainer-facing design notes for `datahub_organization_display_preferences`, the first resource built on DataHub's `globalSettings` singleton. Category 7 of `docs/roadmap.md` reserved this slot for establishing a reusable pattern; this document records the pattern as implemented, the empirical findings behind it, and the scope decisions taken along the way.

## What prompted it

A request to manage "whatever it takes to gain access to the settings on `/settings/preferences`" in a DataHub Cloud instance. Investigating the page turned out to be most of the work: it renders five controls with three different scopes and two different availability tiers, and only two of them belong in this provider.

## The preferences page, control by control

| Control | Scope | OSS/Cloud | Backing mutation | Decision |
|---|---|---|---|---|
| Organization name | org-wide | Cloud-only | `updateOrganizationDisplayPreferences` | **Managed** |
| Organization logo URL | org-wide | Cloud-only | `updateOrganizationDisplayPreferences` | **Managed** |
| Language select | **per-user** | OSS | `updateCorpUserLocaleSettings` | Excluded |
| Sample-data toggle | org-wide | Cloud-only | `updateSampleDataSettings` | Excluded |
| "Show Applications" (Beta features) | org-wide | OSS-stable | `updateApplicationsSettings` | Excluded |

### Why each exclusion

**Language** is stored on `corpUserSettings.locale.language`, per user. The provider's scope rule excludes per-user preferences: they belong to the individual, and Terraform would stomp them on every apply. Not a judgement call.

**Sample-data toggle** is org-wide, so scope alone does not disqualify it, but toggling it is not a configuration write. `SampleDataService` reacts by asynchronously soft-deleting the sample-data entities (and restoring them when toggled back), which makes a declarative `apply`/`destroy` cycle destructive in a way the attribute name does not suggest. It is also only rendered on trial instances (`appConfig.trialConfig.trialEnabled`), though the mutation itself is callable outside a trial - the restriction is client-side only. Excluded on the "not a pure config write" ground.

**"Show Applications" / Beta features** is org-wide, OSS-stable, and would be a clean feature-toggle resource. It is excluded as a deliberate product decision (2026-07-28) rather than a technical one, and is explicitly **not** backlogged: the current view is that this provider should not be the place feature-preview flags get flipped. Revisit only if that view changes.

The upshot: everything in scope is Cloud-only, which is what makes a single resource viable. Had "Show Applications" been included, one resource would have mixed OSS and Cloud fields and partially failed on OSS - the alternative being to split by availability tier.

Also on the same settings *area* but a different route, so out of scope here: `updateHelpLink` (org-wide, Cloud-only, `globalSettingsInfo.visual.helpLink`). It is a reasonable future sibling resource and would reuse this pattern directly.

## Empirical findings (DataHub Cloud, 2026-07-28)

Verified by probing a live Cloud instance rather than inferred from schema. These findings are the reason the resource behaves as it does.

1. **The OpenAPI v3 read path works on the singleton.** `GET /openapi/v3/entity/globalsettings/urn:li:globalSettings:0` returns 200 with `globalSettingsKey` + `globalSettingsInfo`. So Read follows the project's mandated strongly-consistent path; no need to fall back to the Cloud-only `globalSettings` GraphQL query. Note the all-lowercase `globalsettings` path segment.

2. **The mutation is a per-field read-modify-write, not a replace.** Writing only `customOrgName` left `customLogoUrl` intact, and the aspect's unrelated sections (`docPropagation`, `homePage`, `integrations`, `notifications`, `views`) were byte-identical before and after. This was the blocking question: a whole-object write would have silently wiped an org's `helpLink` config, forcing a client-side read-modify-write. It does not.

3. **Fields cannot be removed, only blanked.** An explicit `null` in the mutation input is silently ignored (the stored value is unchanged). An empty string sets the field *to* empty. There is no path back to "absent" once written.

4. **Empty behaves as unset in the UI.** `DataHubTitle.tsx` and `AppConfigProvider.tsx` both select the custom value with a JavaScript `||` chain, and `""` is falsy, so a blank value falls through to DataHub's default title and logo. This is what makes finding 3 tolerable: writing `""` is a genuine reset in effect, even though the field remains present in the aspect.

`appConfig.visualConfig.appTitle` is unrelated server config (`REACT_APP_TITLE`, empty by default) and is not affected by this resource - worth knowing, because reading it as though it reflected `customOrgName` is an easy misdiagnosis.

## The small-singleton pattern

Conventions established here, for reuse by the settings siblings that follow (`helpLink`, doc propagation, global views default, and the remote-executor global config).

- **Hard-coded URN, no user-facing id.** `datahub.GlobalSettingsURN` (`urn:li:globalSettings:0`). `id` and `urn` are both computed and both equal it.
- **Update-only lifecycle.** There is no create: `Create` and `Update` share one `apply` method that moves the server to the configured values. `Delete` resets the managed fields (writes `""`) and removes the resource from state; it never deletes the entity, which is platform-level state DataHub always expects to exist and whose other sections are not this resource's to remove.
- **Read via OpenAPI v3** on the singleton URN. Never a `list*`/search query.
- **Import takes any id.** The URN is fixed, so `ImportState` ignores the supplied id. The importtarget registry entry uses `Enumerate: nil` deliberately: enumeration would be trivially well-defined (one always-present URN), but auto-generating an import for org-wide branding nudges users into a config that blanks whichever field they omit. Importing this should be a deliberate act.
- **Full ownership of every exposed field.** Because `null` cannot clear a field server-side, the client always sends both fields, mapping a null attribute to `""`. Omitting an attribute therefore *resets* it rather than leaving it alone - consistent with the provider's full-map/full-list ownership conventions elsewhere, and documented in the user-facing schema.
- **Canonical empty => null on read.** `canonicalDisplayPreference` keeps an omitted attribute null in state when the server reports empty, so an unset field does not drift `null -> ""` forever. A server value that is empty while the config asked for something is still surfaced, so real drift stays visible.
- **Read-back verification after write**, per the established silent-no-op guard.

### Not every singleton needs a resource

Restating the roadmap's rule because it applied here: prefer a singleton resource when the aspect section carries multiple coherent fields (as `visual`'s name+logo do), or when a flag-on-member would create a cross-instance invariant Terraform cannot check at plan time. A one-field singleton already projected onto another resource's attribute - like the remote-executor default pointer on `datahub_remote_executor_pool.is_default` - does not warrant its own resource.

## Stability posture

The `globalSettings` entity is `category: internal` in `entity-registry.yml`, and the whole read/write surface used here lives only in the closed Cloud fork. Per the project's established convention this reality stays in maintainer-facing docs (here, `CLAUDE.md`, the roadmap); user-facing docs get the `cloudOnlyBadge` plus an ownership-style note, not an "experimental / no stability guarantee" disclaimer. Note also that OSS contributors cannot see the backing source, which matters when triaging an issue against this resource.

## Verification log

- **2026-07-28, DataHub Cloud (demo.acryl.io).** Full mock suite plus live acceptance: `TestAcc_OrganizationDisplayPreferences_Lifecycle` and `..._DataSource` both pass live (set both fields, plan idempotency, in-place update, import, drop-a-field, reset). The OSS-rejection and external-edit tests skip on Cloud by design. After the run, the five unmanaged sections of `globalSettingsInfo` were confirmed byte-identical to the pre-run baseline - including `homePage.defaultTemplate` - and `visual` was left `{"customOrgName": "", "customLogoUrl": ""}`, i.e. behaviourally the instance's original unset state (finding 4).
- **Not yet verified live against OSS.** `TestAcc_OrganizationDisplayPreferences_OSS_RejectsWithCloudOnlyError` asserts the Cloud-only diagnostic rather than a raw GraphQL schema error; it requires an OSS target and is exercised by the nightly Quickstart job.
