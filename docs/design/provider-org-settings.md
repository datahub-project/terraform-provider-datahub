# Org-level settings: the small-singleton pattern

Maintainer-facing design notes for `datahub_organization_display_preferences`, the first resource built on DataHub's `globalSettings` singleton. Category 7 of `docs/roadmap.md` reserved this slot for establishing a reusable pattern; this document records the pattern as implemented, the empirical findings behind it, and the scope decisions taken along the way.

## What prompted it

A request to manage "whatever it takes to gain access to the settings on `/settings/preferences`" in a DataHub Cloud instance. Investigating the page turned out to be most of the work: it renders five controls with three different scopes and two different availability tiers, and only two of them belong in this provider.

## The preferences page, control by control

| Control | Scope | OSS/Cloud | Backing mutation | Decision |
|---|---|---|---|---|
| Organization name | org-wide | Cloud-only | `updateOrganizationDisplayPreferences` | **Managed** |
| Organization logo URL | org-wide | Cloud-only | `updateOrganizationDisplayPreferences` | **Managed** |
| Brand colour | org-wide | Cloud-only (v2.2.0+) | `updateOrganizationDisplayPreferences` | **Managed** (see the `primary_color` section below) |
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
- **Full ownership of every exposed field.** Because `null` cannot clear a field server-side, the client always sends `customOrgName` and `customLogoUrl`, mapping a null attribute to `""`. Omitting an attribute therefore *resets* it rather than leaving it alone - consistent with the provider's full-map/full-list ownership conventions elsewhere, and documented in the user-facing schema. `primary_color` qualifies this once, for compatibility reasons set out in its own section below.
- **Canonical empty => null on read.** `canonicalDisplayPreference` keeps an omitted attribute null in state when the server reports empty, so an unset field does not drift `null -> ""` forever. A server value that is empty while the config asked for something is still surfaced, so real drift stays visible.
- **Read-back verification after write**, per the established silent-no-op guard.

### Why Delete resets rather than restoring or doing nothing

`Delete` is the one operation on an update-only resource with no obviously right answer, so the alternatives are recorded here. The setting always exists, so "delete" cannot mean what it means elsewhere, and the provider has to choose.

The choice made at design time was **clear versus no-op**, and it was contingent on whether DataHub even permitted clearing. It does, but only by writing `""` - an explicit `null` in the mutation input is silently ignored - so clearing won. Restoring the pre-adoption values was not considered at the time. It should have been, if only to be rejected on the record.

**Restore is worse than it first sounds.** It would mean capturing the values at create and holding them in state, and four things go wrong:

- **Import makes it incoherent.** Import adopts the current values, so "previous" and "current" are the same and restore-on-destroy silently degrades to a no-op. Identical configurations would behave differently depending on whether the resource was created or imported.
- **It goes stale.** An admin changing branding in the UI mid-management leaves the stored value predating that change. Restoring resurrects something nobody wants, and only reveals it afterwards.
- **State loss breaks it.** Rebuild state by importing and the remembered value is gone, so `Delete` needs a fallback - which means two behaviours for one operation, selected by state history.
- **It puts undeclared values in state.** State holds desired and actual; this would add history the configuration never mentions and the practitioner cannot see until it surprises them.

More fundamentally, Terraform has no concept of the world before it started managing something. `destroy` removes what was created, and this resource creates nothing.

**No-op was the genuinely arguable alternative**, and is a common shape elsewhere for a resource managing a field on a pre-existing object it did not create. Two things decided against it. Reset is *predictable*: the outcome is the same regardless of what happened while the resource was managed, whereas no-op leaves the instance carrying a value that nothing now owns, and a later re-adoption sees drift against it. And it follows the full-ownership convention above - under no-op the provider would stop owning values that still visibly bear its fingerprints.

The cost of reset is real and must stay documented: **removing the resource from a configuration is an active change, not a withdrawal.** Terraform calls `Delete` for a removed block exactly as it does for `terraform destroy`. A practitioner who wants to stop managing branding without resetting it needs `terraform state rm`, or a `removed {}` block with `destroy = false`. The resource and data source examples both carry that warning, and the data source is the only way to capture the current values beforehand - after a reset, nothing on the instance records what they were, because a reset value and a never-configured one both read back as null.

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

## How the family shares one aspect (added 2026-08-06)

Everything in this family writes into a single aspect, `globalSettingsInfo`, which holds `integrations`, `notifications`, `sso`, `mcpSettings`, `documentationAi`, `aiAssistant`, `aiPlugins`, `aiContext`, `views`, `docPropagation`, `visual`, `homePage`, `applications` and `maintenanceWindow`. Once more than one resource owns a slice of it, the write mechanics stop being per-resource detail and become a shared contract. Established by reading the Cloud resolver on fork HEAD `442d28a7fb2` (2026-08-05).

**`updateGlobalSettings` merges per top-level section, and does not rebuild.** The resolver fetches the existing `globalSettingsInfo` first, then applies each input section only when that input field is non-null, mutating the fetched object. So a caller sending only `ssoSettings` leaves notifications, MCP, the AI groups, `visual` and `homePage` exactly as they were -- including the two sections the mutation cannot express at all.

That matters because the opposite design exists in this same server and has already cost us a guard: `updatePolicy` rebuilds `dataHubPolicyInfo` from its input and silently drops `actors.roles` and `editable` (OSS-1216, `internal/provider/policy_write_guard.go`). Had `updateGlobalSettings` been written that way, a user managing SSO in Terraform would have silently wiped their org branding and home-page default. **It was worth checking rather than assuming, and the same check is owed by any future mutation that writes part of a shared aspect.**

### Write path per section -- prefer the mutation, fall back to the aspect

| Section | Write path | Note |
|---|---|---|
| `integrations`, `notifications`, `sso`, `mcpSettings`, `documentationAi`, `aiAssistant`, `aiPlugins`, `aiContext` | `updateGlobalSettings` | GraphQL, merges per section. The normal rule applies; no exception needed. |
| `visual` | `updateOrganizationDisplayPreferences` | Dedicated mutation, already shipped as `datahub_organization_display_preferences`. |
| `homePage` | **nothing writes it -- by design** | Not a gap. The pointer is bootstrap-seeded at `urn:li:dataHubPageTemplate:home_default_1` and DataHub's own UI edits that template in place rather than moving the pointer. Managed via `datahub_page_template`, not a settings resource. See `provider-home-page-layout.md`. |
| `applications`, `maintenanceWindow` | none identified | Present on the aspect but absent from `UpdateGlobalSettingsInput`. **Do not assume this is a gap** -- that inference was made for `homePage` and was wrong. Check what the DataHub UI does with these first; the absence of a mutation may mean the setting is not meant to be changed that way. |

The rule to follow: **use the dedicated mutation when one exists, and reach for an aspect write only where nothing else can reach the field.** That keeps the service-layer-logic argument behind the provider's write convention intact everywhere it can be, and confines the exception to fields that would otherwise be unmanageable.

**Before concluding a section has no write path, check what the DataHub UI does with it.** This was learned expensively on `homePage`: no mutation exists for the pointer, which was read as an API gap and produced a designed-and-costed resource before anyone looked at the client. The UI never moves that pointer -- it edits the bootstrapped default template in place -- so the missing mutation was the design, not an omission. One grep through `datahub-web-react` would have settled it before any probing. Two sections here (`applications`, `maintenanceWindow`) are currently in the same "no mutation found" state and deserve that check first.

**An aspect write is a usable fallback, but only in one specific shape.** Corrected 2026-08-06 after a second round of probing; an earlier version of this paragraph said `PATCH` on `globalsettingsinfo` fails outright, and that was wrong.

- **`PATCH` with `forceGenericPatch: true` works, and changes only the field you name.** Verified on `globalSettingsInfo`: HTTP 200, one field changed, `docPropagation` and `views` untouched. This is the shape to use for any single-field write to a shared aspect. It has no read-modify-write, so no clobber window against a concurrent UI edit and no need for a serialising mutex.
- **A bare `{"patch": [...]}` fails with an opaque HTTP 500** for any aspect without a registered patch template, which is roughly 315 of the 338 aspect names the spec advertises `PATCH` for. `PatchItemImpl.applyPatch` only selects the generic implementation when `arrayPrimaryKeys` is non-empty or `forceGenericPatch` is set; otherwise it takes the template path, which null-dereferences. Filed upstream as [datahub-project/datahub#18935](https://github.com/datahub-project/datahub/issues/18935).
- **`POST` replaces the entire aspect** and would wipe every section it does not name. Never use it for a single-field write.

So the rule above still prefers a dedicated mutation, but the fallback is much cheaper than previously recorded: a targeted `forceGenericPatch` patch, not a read-modify-write carrying unmanaged sections as opaque JSON.

**One caveat on how far that generalises.** The generic path was verified on one aspect and inferred for the rest from reading `applyGenericPatch`, which constructs a default instance reflectively -- so some aspects may fail there for unrelated reasons. Probe the specific aspect before designing around it.

### Two hazards this creates

**1. Merging means omission cannot clear.** Because the mutation merges into the existing sub-object, a resource that drops a field from its configuration does not clear it server-side -- the old value persists. That is in direct tension with this provider's rule that a resource owns the complete state of what it declares. Each resource in this family must therefore send its section's full desired state, with explicit empties where the user removed something, and it must be verified that an explicit empty actually clears rather than being treated as "absent, leave alone". This is the same shape as the `mcpSettings.servers` merge-by-slug problem, which needed a separate `deleteMcpServer` mutation to become manageable at all.

**2. Concurrent applies within the family can clobber each other.** Every write here is a read-modify-write of the whole aspect, whether performed server-side by the mutation or client-side against the aspect endpoint. Two settings resources applied concurrently -- Terraform's default parallelism is 10 -- can both read the same starting state and the second write can lose the first resource's section. Nothing in the API prevents it, and the loss is silent. Before a second resource in this family ships, decide the mitigation: serialise these writes in the provider client behind a mutex, which is invisible to users and cheap because these are singleton writes, or document `-parallelism=1`, which pushes the problem onto users and will be forgotten. The mutex is the better answer, and it is the reason this section exists before the resources do rather than after someone hits it.

## `primary_color`: the one attribute that is not owned by default (added 2026-09-16)

`primaryColor` arrived in DataHub Cloud v2.2.0, in `globalSettingsInfo.visual` beside `customOrgName` and `customLogoUrl` and writable through the mutation this resource already calls. So there is no new transport, no new domain, and no argument about granularity - it is an attribute on this resource, exactly as the roadmap predicted. What it does change is the ownership rule this document states twice, and the deviation is deliberate.

### Why the established pattern could not simply be copied

The resource's posture is **full ownership of every exposed field**: a null attribute is written as `""`, so omitting one resets it. Applying that to `primary_color` means every write names `primaryColor`, including the writes of users who have never heard of it.

That is a breaking change rather than a new feature. DataHub Cloud is a rolling fleet with a wide deployed version span, and GraphQL coerces a variable's value against the input type *before* executing the operation - so an input-object key the schema does not define fails the whole mutation, whatever the value, including an explicit `null`. Under full ownership, every user of `datahub_organization_display_preferences` on a Cloud instance older than v2.2.0 would find that `terraform apply` had stopped working, on an upgrade that was supposed to add an optional attribute they are not using.

`docs/roadmap.md`, "Version and capability compatibility", already answers this in general terms: the default shape for a new optional attribute on a shipped resource is `Optional` and not `Computed`-with-default, and the protection is to **emit only what the user configured**. This is the first time that rule has collided with a resource whose whole design is that omission means reset.

### The rule as implemented

`primaryColorToWrite(planned, prior)` decides whether the mutation carries `primaryColor` at all:

| Config | Prior state | Sent | Effect |
|---|---|---|---|
| `"#EC0016"` | anything | `"#EC0016"` | set |
| `""` | anything | `""` | clear, restoring DataHub's default brand |
| absent | absent | **nothing** | the field is not named in the mutation |
| absent | present | `""` | clear - removal resets, as it does for the siblings |

And `canonicalPrimaryColor` is the read-side half: a null prior stays null whatever the server holds, where `canonicalDisplayPreference` would adopt the value.

The effect is that `primary_color` is unowned until the practitioner sets it once, and fully owned - identically to `org_name` and `logo_url` - from that moment on, including being reset when the line is later removed or the resource destroyed.

### Why not the two simpler variants

**"Never send it unless configured", with no clear-on-withdrawal**, is what a literal reading of the roadmap rule gives, and it is wrong here. Removing the attribute would produce a plan (state holds a colour, config does not), an apply that reports success, a null written to state - and an instance still showing the old colour, which `canonicalPrimaryColor` then declines to re-adopt. State would diverge from the instance permanently and silently. An apply that does nothing is worse than an apply that refuses.

**"Own it like the siblings"** is the breaking change described above. Note it is not only a compatibility problem: on a v2.2.0 instance it would also mean that adopting this resource to manage an organization's name silently wipes a brand colour set in the UI, because the adopting configuration has no reason to mention `primary_color`.

### What this costs, stated plainly

One resource now has two ownership behaviours, and a reader who knows how `org_name` behaves will guess wrong about `primary_color` exactly once - while it has never been set. The schema description, both examples and the registry page all say so, and the asymmetry is self-limiting: it disappears the moment the attribute is set to anything, including `""`.

`terraform import` adopts a brand colour like any other field, so an imported resource owns it immediately and a configuration omitting it will clear it. That is consistent with the siblings and with the existing warning that importing this resource is a deliberate act.

### Where this generalises, and where it does not

Any future attribute added to an already-shipped resource in this family faces the same question, and the table above is the answer to copy: omit while never configured, clear on withdrawal, adopt on import. What does **not** generalise is the assumption that read is safe. It is safe here only because Read and ImportState use the OpenAPI v3 aspect endpoint, where an absent field is absent JSON and decodes to `""` - the "tolerate mere absence" case. Had Read been the Cloud `globalSettings` GraphQL query, naming `primaryColor` in the selection set would have failed the entire query against every older instance, breaking `terraform plan` for users who had merely upgraded the provider. The project's rule that Read goes through OpenAPI v3 was adopted for consistency, not for compatibility, and it happens to have paid for itself here.

### Validation, and what is not yet known

`primary_color` is validated at plan time by `hexColorOrEmptyValidator`, which delegates to the existing `hexColorValidator` (shared with `datahub_tag.color_hex`, `#RRGGBB`) and exempts `""`. This departs from the preference for server-side rejection recorded in `provider-home-page-layout.md`, and the reason is the failure mode rather than taste: that precedent turns on the server rejecting an unknown module type *cleanly*, whereas **whether DataHub validates a brand colour at all has not been observed**. If it stores `red` verbatim, the frontend derives brand tokens from nonsense and nothing reports it - the "silent" case the roadmap reserves for a guard. If it turns out DataHub accepts other CSS colour forms, the validator is the single thing to relax.

Also unobserved: the exact wording a pre-v2.2.0 Cloud returns when the mutation carries `primaryColor`. `isPrimaryColorUnsupportedError` therefore matches only the field name, which is the part of any graphql-java rejection that can be relied on, and `ErrPrimaryColorUnsupported` wraps the server's message verbatim so a misfire still shows the user what actually went wrong. Guessing at graphql-java's phrasing is precisely how `isOrganizationDisplayPreferencesCloudOnlyError` came to miss the `UnknownType` shape and ship a raw error; the detector is consulted only when the write actually carried the field, which is what makes a loose match safe.

## Stability posture

The `globalSettings` entity is `category: internal` in `entity-registry.yml`, and the whole read/write surface used here lives only in the closed Cloud fork. Per the project's established convention this reality stays in maintainer-facing docs (here, `CLAUDE.md`, the roadmap); user-facing docs get the `cloudOnlyBadge` plus an ownership-style note, not an "experimental / no stability guarantee" disclaimer. Note also that OSS contributors cannot see the backing source, which matters when triaging an issue against this resource.

## Verification log

- **2026-07-28, DataHub Cloud (demo.acryl.io).** Full mock suite plus live acceptance: `TestAcc_OrganizationDisplayPreferences_Lifecycle` and `..._DataSource` both pass live (set both fields, plan idempotency, in-place update, import, drop-a-field, reset). The OSS-rejection and external-edit tests skip on Cloud by design. After the run, the five unmanaged sections of `globalSettingsInfo` were confirmed byte-identical to the pre-run baseline - including `homePage.defaultTemplate` - and `visual` was left `{"customOrgName": "", "customLogoUrl": ""}`, i.e. behaviourally the instance's original unset state (finding 4).
- **Not yet verified live against OSS.** `TestAcc_OrganizationDisplayPreferences_OSS_RejectsWithCloudOnlyError` asserts the Cloud-only diagnostic rather than a raw GraphQL schema error; it requires an OSS target and is exercised by the nightly Quickstart job.
