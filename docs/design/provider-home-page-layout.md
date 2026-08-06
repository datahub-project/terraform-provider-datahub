# Home-page layout: `datahub_page_module` and `datahub_page_template`

Maintainer-facing design for managing DataHub's home-page layout declaratively. Category 13 of `docs/roadmap.md` established that the `GLOBAL` half of this feature is in scope and OSS-stable; this document is the design that follows, with every load-bearing claim probed against a live instance and the server source on 2026-08-06.

## Why, and the use case that sharpens it

The generic argument is the ordinary one for this provider: a `GLOBAL` page template is org-wide presentation configuration, owned by an admin, changed rarely, and gated behind its own privilege. That alone qualifies it.

The argument that makes it worth building sooner is **demo estates**. Every demo estate in use is already fully Terraformed -- domains, glossary, data products, assertions, ingestion sources -- and gets destroyed and rebuilt repeatedly. The landing page is the first thing a prospect sees and is currently the one artefact assembled by hand in the UI, which means it is also the one artefact lost on every rebuild. Modules point at entities the same configuration creates, so Terraform's dependency graph orders it correctly with no extra effort: build the estate, then point the page at it.

## Probe results (demo.acryl.io, DataHub Cloud v2.1.0, commit `db7449e3cd`, 2026-08-06)

| Question | Answer |
|---|---|
| Write path for templates and modules | `upsertPageTemplate`, `deletePageTemplate`, `upsertPageModule`, `deletePageModule` -- all live |
| Read path | `/openapi/v3/entity/datahubpagetemplate/{urn}` and `.../datahubpagetemplateproperties`, same for `datahubpagemodule`. GET available, so the mandated strongly-consistent read path exists |
| Is the URN deterministic? | **Only if we supply it.** See below -- this is the single most important finding |
| Scope enum | `PageTemplateScope` and `PageModuleScope` are both `PERSONAL` / `GLOBAL` |
| Surface enum | `PageTemplateSurfaceType` is `HOME_PAGE` / `ASSET_SUMMARY` / `CONTEXT_DOCUMENTS` |
| Module types | `DataHubPageModuleType`, **22 values at v2.0.3, 30 at v2.1.0** |
| Privilege | `MANAGE_HOME_PAGE_TEMPLATES_PRIVILEGE`, enforced for `GLOBAL` in both the resolver and the service |
| Default-template pointer, GraphQL | **No write path.** `globalHomePageSettings` is a query; `GlobalHomePageSettings` exposes `defaultTemplate` read-only; `UpdateGlobalSettingsInput` has no `homePage` field; `UpdateHomePageSettingsInput` does not exist |
| Default-template pointer, OpenAPI v3 | **Writable.** `/openapi/v3/entity/globalsettings/{urn}/globalsettingsinfo` accepts `POST` and `PATCH`; `GlobalSettingsInfo.homePage` is `optional GlobalHomePageSettings`, and that record is `{ defaultTemplate: Urn }` |
| Per-user pointer | `updateUserHomePageSettings` is writable, but `PERSONAL` scope is out of scope by the provider's per-user rule |
| OSS | Mutations, resolvers, services and both PDLs are all in OSS (`datahub-graphql-core`, `metadata-service/services`, `metadata-models`), and `entity-registry.yml` marks both entity types `category: core`. Expect `ossAndCloudBadge`, pending live Quickstart verification per the OSS checklist |

## The URN determinism finding, and why it governs the design

Both services take a nullable URN and branch on it. `PageTemplateService.upsertPageTemplate`:

```java
if (urn != null) {
  templateUrn = UrnUtils.getUrn(urn);
} else {
  final String templateId = UUID.randomUUID().toString();
  ...
}
```

`PageModuleService` has the identical branch. So this entity type has exactly the non-deterministic creation path the design checklist warns about -- the UI omits the URN and gets a UUID -- **and an adjacent deterministic one that the provider can take unconditionally.**

**The rule for this resource: always send the URN, never omit it.** Both resources therefore take a required, user-supplied `page_template_id` / `page_module_id` and construct `urn:li:dataHubPageTemplate:<id>` / `urn:li:dataHubPageModule:<id>`. That is what makes the demo use case work at all: a rebuilt estate produces the same URNs, so a default-template pointer set once keeps pointing at something real.

Consequence to document for users: a template created in the DataHub UI has a UUID URN. It can be imported, but its id cannot be changed, so importing one means living with the UUID as the Terraform id. Prefer creating from Terraform.

## The default template is edited in place, not pointed at

**There is no third resource, and nothing needs to write `homePage.defaultTemplate`.** The pointer is bootstrap-only by design, and DataHub's own UI never moves it.

Every instance ships a well-known default template and a pointer already aimed at it:

- `metadata-service/war/src/main/resources/boot/global_settings.json` seeds `homePage.defaultTemplate` as `urn:li:dataHubPageTemplate:home_default_1`.
- `metadata-service/configuration/src/main/resources/bootstrap_mcps/page-templates.yaml` seeds `home_default_1` itself, `scope: GLOBAL`, `surfaceType: HOME_PAGE`, with rows over the bootstrapped modules `your_assets`, `top_domains` and `platforms`.
- The Cloud UI's "edit default template" flow (`useTemplateOperations.ts`) calls `upsertPageTemplate` with `scope: GLOBAL` on **that same template**. It edits the default; it does not create a template and repoint anything.

So the model is a fixed default template you *edit*, not a pointer you *move*. Terraform does the same thing the UI does:

```hcl
resource "datahub_page_template" "home" {
  page_template_id = "home_default_1"
  rows             = [...]
}
```

The always-send-URN upsert overwrites the bootstrapped default in place. No aspect write, no read-modify-write, no clobber window, and the demo use case works with the two resources already built.

Two caveats replace the ones this section used to carry, and both are milder:

- **`home_default_1` is a bootstrap constant, not a documented API guarantee.** It is stable in the OSS bootstrap file and identical on Cloud, so depending on it is reasonable -- but it is an implementation detail. Read it from `globalSettingsInfo.homePage.defaultTemplate` as a preflight rather than hardcoding it in a resource, and document that a user managing the default template is adopting a bootstrapped entity.
- **Delete is now the sharp edge.** `terraform destroy` on a template whose id is `home_default_1` removes the instance's default home page and leaves the bootstrapped pointer dangling. That is a worse outcome than the usual "resource goes away". Decide it explicitly: most likely Delete should restore the bootstrap layout rather than delete the entity, or refuse with a diagnostic when the template is the current default. Do not let it fall out of the generic delete path by accident.

**`updateGlobalSettings` will not undo this write.** Checked on fork HEAD `442d28a7fb2` before relying on it: that mutation fetches the existing aspect and applies only the sections named in its input, so `homePage` survives every SSO, notification, MCP or AI-settings write. Had it rebuilt the aspect from its input instead -- the way `updatePolicy` does, which is why OSS-1216 needed a guard -- this resource would have been silently undone by any other settings resource in the family. The wider contract for sharing this aspect, including the concurrency hazard that follows from every write being a read-modify-write, is in `provider-org-settings.md`.

## How this was originally designed, and why that was wrong

Retained deliberately. The first version of this document specified a third resource, `datahub_home_page_settings`, whose whole job was to write `homePage.defaultTemplate`. Two rounds of probing dismantled it, and the wrong turn is worth keeping because the reasoning that produced it looks sound and would be reproduced by the next person.

**The original design:** create a `GLOBAL` template, then set the org-wide pointer at it, the way you would wire up any "which one is active" setting. That framing came from reading the GraphQL surface and finding no mutation for the pointer -- concluding a gap in the API, and designing around it.

**Why it was wrong: the pointer is not meant to move.** DataHub bootstraps `home_default_1` and aims the pointer at it on every instance, and its own UI edits that template in place. There was never a gap to design around. The mistake was inferring intent from the *absence* of a mutation, when the absence was itself the design: no mutation exists because moving the pointer is not an operation DataHub performs. **Reading what the product's own client does would have settled it in one grep, and that is the cheaper check to run first** -- before probing an endpoint, before designing a resource around a supposed gap.

The two probes below are kept because both findings are independently true and both are traps for anyone tempted to write a settings aspect directly. The first is also an upstream bug worth filing on its own merits.

### Probe 1: `PATCH` works, but not in the shape you would reach for first

Verified against a local OSS Quickstart running **v1.7.0** (`make quickstart-up` reused an existing container set rather than the pinned `v1.5.0.6`, so treat the version as v1.7.0, not the Makefile default).

**Corrected 2026-08-06.** This section previously said `PATCH` returns 500 for this aspect "and always will". That was wrong, and wrong in a way worth recording because the same mistake is easy to repeat: it read the template registry and the failing code path, but not the branch above them that chooses between two implementations. **Read the dispatch, not just the handler.**

A bare `{"patch": [...]}` does fail, with an opaque 500 from an unchecked null:

```
java.lang.NullPointerException: Cannot invoke "Template.applyPatch(...)" because "template" is null
  at AspectTemplateEngine.applyPatch(AspectTemplateEngine.java:57)
  at GenericEntitiesController.patchAspect(GenericEntitiesController.java:781)
```

But `PatchItemImpl.applyPatch` selects a **generic** implementation when `arrayPrimaryKeys` is non-empty or `forceGenericPatch` is true, and that one handles any aspect. Verified on `globalSettingsInfo`: HTTP 200, only the named field changed, `docPropagation` and `views` untouched.

So the accurate statement is narrower than the original and more useful: **a bare patch reaches a template path that only ~23 of 338 advertised aspect names have, while `forceGenericPatch: true` works.** The 23 are all asset enrichment -- `ownership`, `globalTags`, `glossaryTerms`, `domains`, `structuredProperties`, `status`, `upstreamLineage` and a scattering of `*Info` records -- and none is a settings aspect, which is why the default path fails here.

The surviving generalisation: **an aspect appearing with `PATCH` in the served spec tells you nothing about which request shape works**, because the spec is generated from the entity registry and cannot express the distinction. Filed upstream as [datahub-project/datahub#18935](https://github.com/datahub-project/datahub/issues/18935), which also covers the opaque 500 hiding three distinct causes.

### Probe 2: `POST` on this aspect replaces all of it

Confirmed by writing only `homePage` and re-reading: `docPropagation` and `views` were both gone. `POST` also requires `?createIfNotExists=false` to touch an aspect that already exists -- the default refuses with a 400, which is easy to misread as a malformed body.

`globalSettingsInfo` is one aspect holding SSO, notifications, integrations, MCP servers, views, documentation-AI, the four AI groups, `visual` and `homePage`. So **any** resource that owns one section and writes it with `POST` wipes every other section on the instance. Restoring the probe's damage by reading the whole aspect, splicing one field and posting it back worked, so a read-modify-write is mechanically sound -- but it is the shape of write that has to be got exactly right, and this resource no longer needs it.

**What this means for anything that does still need an aspect write** (`applications` and `maintenanceWindow` are the current candidates, both lacking a mutation): **use `PATCH` with `forceGenericPatch: true`, not a read-modify-write.** Probe 1 above establishes that it changes only the field named and leaves siblings intact, which avoids the whole class of problem -- no unmanaged sections to carry, no clobber window against a concurrent UI edit, nothing to serialise.

Read-modify-write via `POST` remains the fallback of last resort if a specific aspect turns out to reject generic patching. If it is ever needed, the untouched sections must be carried as **opaque JSON, never a typed struct**: decoding into Go before writing back silently destroys every field the struct does not model -- the `updatePolicy` / `actors.roles` failure shape, self-inflicted, landing on SSO and notification configuration. And such writes must be serialised, since two concurrent read-modify-writes against one aspect lose one update outright with no compare-and-set to detect it.

**`updateGlobalSettings` does not undo an aspect write**, checked on fork HEAD `442d28a7fb2`: it fetches the existing aspect and applies only the sections named in its input, so unrelated sections survive. Worth having established, because the opposite design exists in the same server -- `updatePolicy` rebuilds from input and drops `actors.roles` -- and if this mutation behaved that way, every settings resource would silently undo the others. The wider contract is in `provider-org-settings.md`.

## Resources

**Two**, not the three this document originally specified:

**`datahub_page_module`** -- `page_module_id` (required), `name`, `type`, `scope`, `params`. One resource per module. Modules are referenced by templates, so they are separate resources rather than nested blocks: a module is independently addressable in the API, can be shared between templates, and Terraform's graph then orders module-before-template from the reference alone.

**`datahub_page_template`** -- `page_template_id` (required), `scope`, `surface_type`, and `rows`, an ordered list where each row holds an ordered list of module URNs.

~~**`datahub_home_page_settings`**~~ -- **dropped.** Nothing needs to write the pointer; managing `home_default_1` with `datahub_page_template` is what the DataHub UI itself does. See "The default template is edited in place".

Plus data sources for template and module lookup, and the usual plural enumerator if it is cheap.

### `type` is a string, not a hardcoded enum

The module-type enum went from 22 values to 30 in one Cloud release, adding `AI_CONTEXT`, `METRIC_SQL`, `OUTPUT_PORTS`, `RELATED_METRICS` and four `SEMANTIC_MODEL_*` types. A hardcoded Go enum would have made all eight unusable until the provider cut a release, putting the provider on the critical path for every server-side addition.

So accept `type` as a free string and let the server reject an invalid value, translating the error. This is the version-and-capability-compatibility convention in `docs/roadmap.md` applied directly: the old server rejects cleanly, so translate rather than probe or gate. Document the known types rather than enforcing them.

Note that only five types carry parameters (`LINK`, `RICH_TEXT`, `ASSET_COLLECTION`, `HIERARCHY` via `hierarchyViewParams`, `AGENT_CARD`); the rest are parameterless. `params` is therefore optional, and the provider must not require it.

### `rows` is a nested attribute, which is where this resource can go wrong

`rows` is a list of objects each containing a list of strings. Per the "Unknown Values in Nested Attributes" section of `docs/design/datahub-model-and-resource-design.md`, model it with `types.List` and `types.Object`, never `[]rowModel`. A Go slice cannot hold an unknown, so a `rows` block fed from a variable or `for_each` -- which is exactly how a demo estate would drive it -- fails conversion before the provider runs. This class of bug has shipped twice in this provider.

That also makes the non-literal test mandatory rather than optional: one test step using `ConfigVariables`, or feeding from `terraform_data.seed.output`, because a literal in a test config resolves schema defaults and never produces an unknown. The whole suite can pass while every module use is broken.

**Aspect-list ownership applies.** The template owns its complete row list and always writes the full desired state. A module added to a template outside Terraform is removed on the next apply. Document it.

### Scope is GLOBAL only

`PERSONAL` templates are per-user preferences, excluded by the provider-scope rule. Reject `scope = "PERSONAL"` in `ValidateConfig` with a diagnostic explaining why, rather than silently accepting a value that would write a template nobody can administer. `updateUserHomePageSettings` stays unimplemented for the same reason.

Worth naming the tension honestly, since it will come up: for a demo where the presenter drives, the *presenter's own* `PERSONAL` page is what the audience sees, and that pointer **is** writable today while the `GLOBAL` one needs an out-of-band step. If we ever want it, it should be a deliberate, argued exception to the per-user rule -- not something that arrives because it happened to be the path that worked.

### `surface_type`

Support `HOME_PAGE` first. `ASSET_SUMMARY` and `CONTEXT_DOCUMENTS` are different product surfaces that happen to share the entity type; accepting them without understanding them invites a resource that claims more than it verifies. Accept the attribute, default it to `HOME_PAGE`, and validate against the three known values -- unlike `type`, this enum is small, stable and tied to product surfaces rather than a growing catalogue of widgets.

## Open questions to resolve during implementation

~~1. **Does `PATCH` on `globalsettingsinfo` merge or replace?**~~ **Answered: neither.** `PATCH` is unavailable for this aspect (probe 1) and `POST` replaces all of it (probe 2). Moot for this feature now that no pointer write is needed.

~~2. **Can `defaultTemplate` be cleared?**~~ **Moot** -- nothing writes the pointer.

3. **Does a template survive its modules being deleted?** If a module delete leaves a dangling URN in a template's rows, destroy ordering matters and the provider may need the teardown-ordering care that structured-property defaults needed. Untested; the mock cannot answer it because it stores exactly what it is given.
4. **What does the API do with an empty `rows` list?** Covered by a mock test, not yet against a live server. A template with no rows may be rejected outright, or may render an empty page.
5. **What should Delete do when the template is the instance default?** Replaces the two struck-out questions as the sharp edge. Destroying `home_default_1` removes the instance's home page and leaves the bootstrapped pointer dangling. Candidates: restore the bootstrap layout, refuse with a diagnostic, or delete and warn. Needs a decision plus a live check on whether the bootstrap step recreates the entity on restart (`changeType: CREATE` suggests it would, but only on restart, leaving a window with no home page).
6. **Live OSS verification** against a Quickstart, per the OSS checklist, before claiming `ossAndCloudBadge`.

## Execution phases

Ordered so that each phase is independently useful and the riskiest unknown is settled first.

1. **Probe phase.** Answer open questions 1, 2 and 4. Question 1 gates the third resource entirely. **Probe on a local OSS Quickstart, not on demo.acryl.io:** the questions concern `globalSettingsInfo`, whose other sections hold that instance's SSO, notification and integration configuration, and a mis-shaped `POST` there would break a shared demo environment rather than a disposable container. The aspect and both PDLs are OSS, so a Quickstart answers the merge question faithfully. Only the Cloud-only sections would need a Cloud probe, and those are out of scope for this resource.
2. **`datahub_page_module`** plus data source. Self-contained, no nesting, deterministic URN, exercises the string-typed `type` decision.
3. **`datahub_page_template`** plus data source. The nested-attribute work, including the non-literal test.
4. ~~**`datahub_home_page_settings`**~~ **Rescoped 2026-08-06 -- no third resource. Phase 4 became default-template adoption, and is complete**, which was smaller and safer than what it replaced. Delivered as a **read-only `datahub_home_page_settings` data source** exposing `default_template_urn` and `default_template_id`, plus the delete guard. A data source rather than a resource is the point: it lets a configuration discover which template to manage without the provider ever writing the pointer. What it covers:
   - **Delete semantics for the instance default** (open question 5). The only genuinely risky part of this feature, and the one thing that could leave a user without a home page.
   - **Preflight-read the pointer** rather than hardcoding `home_default_1`, so a user learns which template is actually the default on their instance instead of the provider assuming.
   - **Document the adoption pattern**: managing the default template means adopting a bootstrapped entity, with an `import` block as the tidy route and the delete caveat stated plainly.
   - **Live-verify the in-place overwrite** on a Quickstart: upsert `home_default_1` with our own rows and confirm the rendered home page changes, which is the end-to-end claim the whole feature rests on and which no mock can establish.
5. **A runnable example -- complete.** `examples/runnable/home-page-layout`, slug `homepage`. Four modules over three rows, `datahub_home_page_settings` feeding the template id, and outputs including `is_the_live_home_page` so an operator can tell at a glance whether what they applied is what users see.

   **It defaults to *not* touching the real home page**, creating `tf-example-homepage-demo` instead, with `adopt_default_template = true` as the opt-in. That default is deliberate and load-bearing rather than timidity: an example that adopted the default template would rewrite the home page of any shared instance it was applied to, and its `terraform destroy` would then be refused by the guard above -- leaving a runnable example that cannot clean up after itself, which is exactly what the live-execution harness in `live-example-execution.md` needs to be able to do. The cost is that the default mode demonstrates something nobody sees, which the README states plainly rather than glossing.

**Live verification status.** The server-side claim is proven: on a v1.7.0 Quickstart, `upsertPageTemplate` with `scope: GLOBAL` against the bootstrapped default replaced its rows in place, the pointer still named it afterwards, and the neighbouring `globalSettingsInfo` sections were untouched. What remains unverified is the last inch -- nobody has loaded a DataHub home page in a browser and confirmed it renders the applied layout. Everything between the write and the render is DataHub's own path, and the UI reads from the same aspect, so the risk is low; it is recorded rather than claimed.

**And one item leaves this plan.** The client-side mutex for `globalSettingsInfo` writes is no longer needed here, because this feature no longer writes that aspect at all. It remains a real requirement for the wider settings family and is recorded in `provider-org-settings.md`; the first resource that actually writes a section of that aspect owns it. Worth noting that dropping the third resource is what removed the concurrency hazard, the opaque-JSON requirement and the clobber window in one go -- a fair sign the rescope is the right shape rather than a convenient one.
