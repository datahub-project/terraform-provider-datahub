# Home-page layout: `datahub_page_module`, `datahub_page_template`, `datahub_home_page_settings`

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

## The pointer, and a documented exception to the write rule

The provider's standing rule is GraphQL for Create/Update/Delete, because OpenAPI writes bypass service-layer business logic. `datahub_home_page_settings` cannot follow it: **there is no GraphQL write for `defaultTemplate` at all.** The choice is an OpenAPI v3 aspect write or no resource.

Take the OpenAPI write, and record the reasoning: the field is a bare URN pointer on a settings aspect with no service-layer behaviour behind it -- no encryption, no derived aspects, no side effects of the kind that motivated the rule. This is the same shape as the `assertionInferenceAdjustmentRule` question parked in the roadmap, resolved in the affirmative here because the field is trivially simple.

**The hazard, and it is severe: use `PATCH`, never `POST`.** `globalSettingsInfo` is a single aspect holding SSO, notifications, integrations, MCP servers, views, documentation-AI and the four AI groups. `POST` replaces the whole aspect, so a resource that owned only `homePage` and wrote via `POST` would silently wipe every unrelated setting on the instance. `PATCH` is what makes a single-field write safe. This is the identical trap `docs/design/provider-org-settings.md` identified for the `visual` sub-object, and it must be resolved the same way -- by writing only the field, and by verifying against a live instance that neighbouring settings survive the write.

## Resources

Three, following the granularity rule (group by administrative domain, not by UI page and not by mutation):

**`datahub_page_module`** -- `page_module_id` (required), `name`, `type`, `scope`, `params`. One resource per module. Modules are referenced by templates, so they are separate resources rather than nested blocks: a module is independently addressable in the API, can be shared between templates, and Terraform's graph then orders module-before-template from the reference alone.

**`datahub_page_template`** -- `page_template_id` (required), `scope`, `surface_type`, and `rows`, an ordered list where each row holds an ordered list of module URNs.

**`datahub_home_page_settings`** -- singleton on `urn:li:globalSettings:0`, one attribute `default_template_urn`. Update-only lifecycle, no create, no entity delete, exactly as `datahub_organization_display_preferences` established.

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

1. **Does `PATCH` on `globalsettingsinfo` merge or replace?** The whole safety of `datahub_home_page_settings` rests on it. Probe by reading the aspect, patching only `homePage.defaultTemplate`, and re-reading to confirm SSO/notifications/MCP survived. Do this before writing the resource, not after.
2. **Can `defaultTemplate` be cleared?** Decides whether Delete clears the pointer or is a documented no-op with a warning. Same question `datahub_organization_display_preferences` had to answer.
3. **Does a template survive its modules being deleted?** If a module delete leaves a dangling reference in a template's rows, destroy ordering matters and the provider may need the same kind of teardown-ordering care that structured-property defaults needed.
4. **What does the API do with an empty `rows` list?** A template with no rows may be rejected, or may render an empty page.
5. **Live OSS verification** against a Quickstart, per the OSS checklist, before claiming `ossAndCloudBadge`.

## Execution phases

Ordered so that each phase is independently useful and the riskiest unknown is settled first.

1. **Probe phase.** Answer open questions 1, 2 and 4 against demo.acryl.io. Question 1 gates the third resource entirely.
2. **`datahub_page_module`** plus data source. Self-contained, no nesting, deterministic URN, exercises the string-typed `type` decision.
3. **`datahub_page_template`** plus data source. The nested-attribute work, including the non-literal test.
4. **`datahub_home_page_settings`.** Only if question 1 resolves safely; otherwise stop and record why, and the feature ships with a documented manual step to set the default.
5. **A runnable example** assembling a small home page over entities it creates -- which is also the demo-estate pattern, and the thing that proves the three resources compose.
