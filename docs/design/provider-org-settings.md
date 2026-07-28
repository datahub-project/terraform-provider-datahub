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

Also org-wide and Cloud-only, and stored in the same `globalSettingsInfo.visual` section, but surfaced on a different route so not built here: `updateHelpLink`. When it is built it becomes additional attributes on *this* resource rather than a resource of its own - see "Resource granularity and naming" below.

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

## Resource granularity and naming

The question this section answers: DataHub has a lot of org-level settings. How many Terraform resources should they become, where are the boundaries, and what do we call them?

### Two anchors that look obvious and are both wrong

**Not the UI page.** The obvious reading of "build a resource for `/settings/preferences`" is one resource per settings page. That page alone spans three different storage homes and two scopes:

| Control | Stored in | Scope |
|---|---|---|
| Org name, logo, sample data | `globalSettingsInfo.visual` | org-wide |
| Show Applications | `globalSettingsInfo.applications` | org-wide |
| Language | `corpUserSettings.locale` | **per-user** |

A page is a UI grouping chosen for human browsing, it mixes things Terraform must treat differently, and DataHub can rearrange it in any release. It is not a model.

**Not the GraphQL mutation.** The first rule proposed for this resource was one-resource-per-mutation, on the reasoning that a mutation is the server's own write unit and therefore a natural transaction boundary. It survived about a day. `updateGlobalSettings` takes SSO, notifications, integrations, and four separate AI settings groups as sub-inputs of a single mutation, so the rule would fuse SSO config and AI instructions into one resource. Display preferences only *looked* like a clean fit because DataHub happens to have given it a dedicated narrow mutation. Mutation granularity is an artifact of how the API grew, not a statement about how the settings relate.

### The rule: group by administrative domain

**A settings resource covers one coherent administrative domain: the set of settings that one persona owns, one privilege gates, and one availability tier applies to.** Aspect sections (`visual`, `notifications`, `integrations`, the AI group) are the raw material, since they are DataHub's own grouping and are stable, but merge several where one domain spans them and split one where it clearly holds two unrelated concerns.

Name the resource after the domain. `datahub_organization_display_preferences` maps to `globalSettingsInfo.visual` - a better anchor than either the page or the mutation, and the reason the name survives the page being reorganised.

### Why not one fat `datahub_global_settings`

The alternative - a single settings resource with an attribute per field - is a real pattern in the ecosystem (`gitlab_application_settings`, `github_organization_settings`, `datadog_organization_settings`), and the counter-pattern is equally real: AWS deliberately split the monolithic `aws_s3_bucket` into around twelve standalone resources in v4, accepting a breaking major version, because the fat resource had become unmaintainable with poor ownership semantics. Precedent alone does not decide it. Four properties of *this* provider do:

1. **Ownership semantics.** This provider's convention is that a resource owns what it declares, and omitting an attribute resets it. A fat settings resource means managing the org logo hands you a resource that also owns SSO, notifications and AI instructions - omit them and they reset. Avoiding that needs "null means do not manage", which contradicts the convention, makes clearing a field impossible, and removes drift detection. This is the decisive one: it is a semantic breakage, not a style preference.
2. **Privilege granularity.** `MANAGE_ORGANIZATION_DISPLAY_PREFERENCES`, `MANAGE_GLOBAL_SETTINGS` and `MANAGE_FEATURES` are distinct. A fat resource fails wholesale when the token lacks any one of them, including for fields the config never set.
3. **Availability tiers.** Already load-bearing here: org branding is Cloud-only while Show Applications is OSS. One resource spanning both half-fails on OSS.
4. **Team ownership.** Platform owns SSO, governance owns AI instructions, brand owns the logo. One resource is one state entry with one owner; separate resources let those teams coexist without fighting over a single object.

The usual argument for consolidation - fewer resource names to remember - cuts the other way in practice. Registry docs are per-resource pages, so a sixty-attribute settings resource is a wall of prose, and editor completion works on resource type names, so `datahub_` surfaces the domains while a nested attribute nobody knows about cannot be completed into.

### Consequences already decided

- **`helpLink` folds into `datahub_organization_display_preferences`** when it is built. It lives in `globalSettingsInfo.visual`, it is a display affordance, and it is gated and tiered identically. It does not become `datahub_help_link`. This also removes the resource's only naming imprecision: with the help link included, the resource covers `visual` rather than an arbitrary two of its four fields.
- **A future org-wide dark/light mode** is a new attribute on this resource, not a new resource, for the same reasons.
- **A non-display setting appearing on the preferences page** becomes its own domain resource. The page is not the anchor, so it can gain or lose controls freely.
- **AI settings become one `datahub_ai_settings`**, covering `aiAssistant` instructions, `documentationAi`, `aiContext` evaluation and the MCP telemetry flag - one persona, one page, one tier - rather than four resources mirroring four aspect sections.
- **`aiPlugins` and `mcpServers` are lists of objects, not scalars.** List-valued configuration whose members have independent lifecycles is the classic case for a child resource (`datahub_ai_plugin`, one per plugin), not an attribute on the settings resource.
- **SSO is argued against entirely.** Terraform managing the mechanism that authenticates Terraform is a lockout waiting to happen; the recovery path is manual regardless, so the resource buys little.

Expected end state is roughly four settings resources, not ten: display preferences, AI settings, notification settings, integration settings.

### The org-versus-user trap recurs

Every DataHub settings page seen so far mixes org-wide and per-user settings, and the AI settings page is no exception - `updateCorpUserAiAssistantSettings`, `updateUserAiPluginSettings` and `updateUserContextDocumentsSettings` sit alongside the global ones. **Check the scope of every control before modelling a settings page.** Per-user settings stay out per the provider's scope rule; only the system-level half is ever in scope.

## Stability posture

The `globalSettings` entity is `category: internal` in `entity-registry.yml`, and the whole read/write surface used here lives only in the closed Cloud fork. Per the project's established convention this reality stays in maintainer-facing docs (here, `CLAUDE.md`, the roadmap); user-facing docs get the `cloudOnlyBadge` plus an ownership-style note, not an "experimental / no stability guarantee" disclaimer. Note also that OSS contributors cannot see the backing source, which matters when triaging an issue against this resource.

## Verification log

- **2026-07-28, DataHub Cloud (demo.acryl.io).** Full mock suite plus live acceptance: `TestAcc_OrganizationDisplayPreferences_Lifecycle` and `..._DataSource` both pass live (set both fields, plan idempotency, in-place update, import, drop-a-field, reset). The OSS-rejection and external-edit tests skip on Cloud by design. After the run, the five unmanaged sections of `globalSettingsInfo` were confirmed byte-identical to the pre-run baseline - including `homePage.defaultTemplate` - and `visual` was left `{"customOrgName": "", "customLogoUrl": ""}`, i.e. behaviourally the instance's original unset state (finding 4).
- **Not yet verified live against OSS.** `TestAcc_OrganizationDisplayPreferences_OSS_RejectsWithCloudOnlyError` asserts the Cloud-only diagnostic rather than a raw GraphQL schema error; it requires an OSS target and is exercised by the nightly Quickstart job.
