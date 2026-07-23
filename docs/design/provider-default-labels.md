# Provider-Level Default Labels

Status: in progress. This document records the design for provider-level default labels (custom properties, structured properties, tags) and the auto-property provenance markers. The feature ships incrementally; the "Rollout" section tracks which parts are live.

## Motivation

Operators managing DataHub configuration across multiple Terraform modules or stacks need a way to mark and distinguish Terraform-managed entities: at minimum a `managed-by = "terraform"` provenance marker, and more generally per-stack labels ("which TF project owns this domain tree?"). The AWS provider's `default_tags` and the Google provider's `default_labels` + `goog-terraform-provisioned` attribution label are the established precedents.

## Scope rationale

`docs/roadmap.md` deny-lists per-asset enrichment (tags/terms/owners on datasets, charts, dashboards). That stance targets ingest-discovered entities owned by business users, whose edits Terraform would stomp. This feature is different in kind: it labels only entities the provider itself creates and fully manages (domains, glossary, data products, users/groups, contracts, assertions). The deny-list is unaffected; nothing here adds enrichment capability for data assets.

## Server-side support matrix

Derived from the DataHub entity registry (`metadata-models/src/main/resources/entity-registry.yml`). An aspect write against an entity type that does not register the aspect is rejected or silently dropped by the server, so support is determined entirely by the registry:

| Provider resource | Entity type | Custom properties | Structured properties | Tags (`globalTags`) |
|---|---|---|---|---|
| `datahub_domain` | domain | yes | yes | no |
| `datahub_glossary_term` | glossaryTerm | yes | yes | no |
| `datahub_glossary_node` | glossaryNode | yes | yes | no |
| `datahub_corp_user`, `datahub_service_account` | corpUser | yes | yes | yes |
| `datahub_corp_group` | corpGroup | no (`corpGroupInfo` has no customProperties) | yes | yes |
| `datahub_data_product` | dataProduct | yes | yes | yes |
| `datahub_data_contract` | dataContract | no | yes | no |
| assertion resources (custom/field/freshness/schema/sql/volume) | assertion | excluded (see below) | no | yes |
| everything else (`datahub_ingestion_source`, `datahub_secret`, `datahub_policy`, `datahub_connection`, `datahub_tag`, `datahub_ownership_type`, ...) | various | none | none | none |

Notes:

- Assertions register `customProperties` server-side, but the value is invisible in the DataHub UI and GraphQL API (established during the v0.14.0 custom-properties work), so CP defaults are deliberately withheld there.
- Custom properties on domains are stored and API-queryable but do not currently render in the domain UI (GraphQL DomainProperties gap). The UI-visible and search-filterable marker path is a structured property (see "Filterable marker recipe" below).
- The flagship resources (ingestion sources, secrets, policies, connections) support none of the mechanisms. We explicitly decided against free-text stowage there: recipes are executable configuration (DataHub's ingestion config model is pydantic `extra="forbid"`, so an injected key breaks every run), and embedding markers in description fields pollutes user-facing content and breaks on UI edits. The durable fix is an upstream entity-registry change registering `structuredProperties` on those entity types; that is future work outside this feature.

## Decided semantics

- Mechanisms: custom properties, structured properties, and tags. Glossary terms were considered and dropped (only dataProduct supports them among managed types).
- Collisions: resource-level values win per key over provider defaults, with a plan-time warning emitted only when the values differ (same-value overlap is harmless layering and warning on it every plan trains users to ignore warnings).
- External edits: the provider owns the complete list. Custom properties: full-map ownership (existing convention). Tags: full `globalTags` aspect ownership while defaults are set, guarded by an ownership latch so a provider upgrade alone never strips UI-applied tags. Structured properties: per-property ownership (the `datahub_structured_property_assignment` resource legitimately co-manages the same aspect, and the server API is a per-property merge).

## Provider configuration surface

```hcl
provider "datahub" {
  # Provenance markers (top-level, independent of the defaults block):
  # auto_properties        = ["managed-by"]      # default; [] disables
  # auto_property_strategy = "CREATION_ONLY"     # default; or "PROACTIVE"

  defaults = {
    custom_properties     = { team = "data-platform" }
    tags                  = ["urn:li:tag:terraform-managed"]
    structured_properties = { "urn:li:structuredProperty:io.example.stack" = ["prod"] }
  }
}
```

### Auto properties (provenance markers)

Modeled on the Google provider's `add_terraform_attribution_label` / `terraform_attribution_label_addition_strategy` (opt-in at 5.16, default-on since 6.0). Deliberately decoupled from the `defaults` block: an early design activated markers via the presence of `defaults = {}`, which reads equally plausibly as "I want empty defaults" -- config whose literal reading is ambiguous was rejected. As independent attributes, adding or changing `defaults` never silently affects the markers.

- `auto_properties`: set of marker names, default `["managed-by"]` (on by default). Markers write into the custom-properties merge at the lowest precedence. v1 markers: `managed-by` -> `"terraform"`, `provider-version` -> the running provider version.
- `auto_property_strategy`:
  - `CREATION_ONLY` (default): markers are stamped only when an entity is created, and their values are frozen at creation (a `provider-version` stamp is a birth certificate and never drifts). Upgrading the provider produces zero diffs on existing resources. The estate converges toward fully-stamped over time (rebuilds, replacements), so marker absence does not prove non-management until a PROACTIVE pass has run.
  - `PROACTIVE`: markers and their live values are enforced on every managed entity on every plan. Run once to converge an existing estate; leave on to keep `provider-version` current (accepting a diff wave per provider upgrade).
- Removing a marker name from `auto_properties` (including `[]`) removes the property estate-wide on the next apply regardless of strategy: an explicit flag change is expressed intent, unlike the implicit upgrade the strategy fence protects against.
- Explicit keys of the same name always win: `defaults.custom_properties` overrides a marker silently (that is the documented way to change a marker's value); a resource-level key overrides both (with a warning when values differ).
- `lifecycle.ignore_changes` cannot suppress marker changes (they land in a computed attribute via provider plan modification, which `ignore_changes` does not intercept); the strategy attribute is the drift-control knob.

### Precedence (lowest to highest)

1. Auto-property markers
2. `defaults.custom_properties`
3. Resource-level `custom_properties`

## State model

The load-bearing invariant, learned from years of AWS `default_tags` bugs ("Provider produced inconsistent final plan", perpetual diffs): merged values are computed in resource `ModifyPlan` as a pure function of resource config, prior Terraform state, and provider configuration -- never of server-side data. Framework facts verified against terraform-plugin-framework v1.19.0: resource `Configure` runs inside every `PlanResourceChange`, so provider defaults are available to `ModifyPlan`; when only provider defaults change, `ModifyPlan` writes a new merged value that differs from state, producing a real diff.

Per mechanism (implemented in later rollout phases):

1. `custom_properties_all` (computed map on CP-supporting resources): plan value = merge(markers, defaults, config); Read stores the full server map; the user-facing `custom_properties` attribute stays a pure echo of config. Empty merges canonicalise to null everywhere (one `canonical` rule shared by plan and read paths, otherwise `{}` vs null is a perpetual diff).
2. `tags_all` (computed set on tag-supporting resources) with an ownership latch: the provider owns the entity's full `globalTags` list iff state `tags_all` is non-null. With no defaults configured and a null state the feature is fully inert (no reads, no writes, external tags invisible) -- this is what makes a provider upgrade safe. Removing `defaults.tags` later plans `tags_all -> null`, clears the aspect, and releases the latch.
3. `structured_properties_defaults` (computed map on SP-supporting resources; deliberately not named `_all` because it is not the full server view): per-property ownership. `ModifyPlan` filters default property URNs by the definition's declared `entityTypes` (definitions fetched once at provider `Configure`); Read fetches only property URNs already in state, so assignment-resource-managed and external properties are invisible to it, and vice versa.

No `UseStateForUnknown` on any of these computed attributes -- they must go unknown/recomputed whenever inputs change. Every `ModifyPlan` returns early on destroy plans (`req.Plan.Raw.IsNull()`).

## Plumbing

- `internal/provider/provider_data.go`: `providerData` (embeds `*datahub.Client`, carries `entityDefaults`) is what provider `Configure` sets as `resp.ResourceData`. Data sources continue to receive the bare client. `resourceProviderData()` is the shared Configure helper every resource uses.
- `internal/provider/defaults.go`: parsed configuration (`entityDefaults`), the `entityKind` support matrix (`defaultsSupport`, guarded by a completeness unit test), the pure merge functions, and the validators for the new provider attributes.
- Unknown provider defaults are preserved, not rejected (unlike `gms_url`): they flow into "known after apply" and resolve during the apply-time replan. Referenced tag/SP definitions must pre-exist; the recommended pattern is creating them in a separate bootstrap apply.

## Filterable marker recipe

Custom properties are free-text indexed but not a search facet. For a search-filterable marker on all SP-supporting resources:

```hcl
resource "datahub_structured_property" "managed_by" {
  # id: io.terraform.managedBy, value type STRING,
  # entity_types: domain, glossaryTerm, glossaryNode, corpuser, corpGroup, dataProduct, dataContract
}

provider "datahub" {
  defaults = {
    structured_properties = { (datahub_structured_property.managed_by.urn) = ["terraform"] }
  }
}
```

(Two applies: the definition must exist before the provider defaults reference it.)

## Empirical verification log

To be verified against a live DataHub during rollout and recorded here:

- [x] `globalTags` OpenAPI v3 write acceptance and persistence for corpuser, corpgroup, and dataproduct - verified 2026-07-15 against DataHub Cloud (demo.acryl.io): all three lowercase path segments accepted, writes persisted and read back exactly (latch lifecycle including clear-on-unlatch, create-time tagging, import while latched). The assertion entity path remains to be verified in the assertion-tags phase.
- [x] Nonexistent tag URN in `defaults.tags` - the provider's `ensureTagsExist` guard fails fast before any write (verified live 2026-07-15), so the server's acceptance of dangling tag references is moot for the provider. The raw server behavior itself was not probed.
- [ ] Structured property upsert on the three new assignment target types (corpuser, corpGroup, dataContract).
- [ ] corpUser custom-properties write path merge-vs-replace semantics (different writer than the domain-style full-aspect POST).

Environmental note from the 2026-07-15 run: the GraphQL `deleteDataProduct` mutation returned "Unauthorized" for the test token on the Cloud tenant (post-test destroy failure only; the tag write path on dataproduct passed). Live runs of data-product tests need a token whose principal can delete data products, or manual cleanup afterwards.

2026-07-23 run (assertion path): `globalTags` writes on the assertion entity verified against DataHub Cloud - accepted, persisted, read back exactly; full latch lifecycle on `datahub_custom_assertion` including unlatch. One live hazard surfaced: destroying a marker tag in the same apply as entities still carrying it races DataHub's async `deleteReferences` cascade - the cascade's stale-graph-scroll full-aspect upsert resurrects a just-deleted entity as a husk (filed as CAT-2701, a sibling of CAT-2583 with a different write mechanism; observed on a freshness assertion, husk = `assertionKey` + empty `globalTags`). This affects real users too: a `terraform destroy` of a config holding both a `datahub_tag` used in `defaults.tags` and latched resources can leave husk debris. Mitigations: remove `defaults.tags` (unlatch apply) before destroying the tag, or destroy in two passes; the PR7 guide must document this. The acceptance scenarios unlatch before destroy for this reason (CAT-2701 marker in `datahubtesting/default_tags.go`); remove that ordering constraint when CAT-2701 ships.

## Rollout

| Phase | Content | Status |
|---|---|---|
| 1 | Plumbing: providerData wrapper threaded to all resources, defaults engine + validators + unit tests, this document. No user-visible changes; the provider schema is deliberately withheld so this phase is release-safe on its own. | shipped (#86) |
| 2 | Provider schema (`defaults`, `auto_properties`, `auto_property_strategy`) + custom-property defaults and auto-properties on the 6 CP resources (`custom_properties_all`) | shipped (#87) |
| 3 | `globalTags` client + `defaults.tags` on corp_user/service_account/corp_group/data_product (`tags_all` ownership latch, tag-existence guard, read-back verified writes) | this change |
| 4 | Tag defaults on the 6 assertion resources (`tags_all` latch reuse; assertion entity path pending Cloud verification - the read-back write guard turns a CAT-2562-style silent no-op into an explicit error until then) | this change |
| 5 | Assignment-target extension (corpuser, corpGroup, dataContract). Registry short name for users is all-lowercase `corpuser`. | this change |
| 6 | Structured-property defaults (`structured_properties_defaults`, per-property ownership latch; definitions prefetched at Configure with warn-and-skip on missing so destroy is never blocked; apply-time re-check hard-errors on writes) | this change |
| 7 | Docs guide (`guides/provider-defaults`), provider example, structured-and-custom-properties README note, roadmap note | this change |
| 8 (proposed) | `defaults.owners`: opt-in default ownership (owner URN + ownership type URN) via the `ownership` aspect. This is the only mechanism available today on `dataHubIngestionSource` (and `tag`), which register `ownership` but no label aspects - the works-now answer to marking ingestion sources as Terraform-managed while CAT-2642 (registering structuredProperties on the entity) waits on a server release. Requires a pre-existing owner principal (e.g. a `datahub_service_account`) and a custom `datahub_ownership_type` (e.g. "Provisioned By") to keep provenance distinct from human responsibility. Never on by default: ownership is user-facing governance metadata. | pending design |

Every phase is independently mergeable and release-safe: no phase ships schema without behavior. Phase 2 must document (in the `defaults` attribute description) that tag and structured-property defaults are accepted only from the phases that implement them; alternatively phase 2 exposes only the attributes it wires (`custom_properties`, `auto_properties`, `auto_property_strategy`) and later phases add `defaults.tags` / `defaults.structured_properties` when their write paths land. The latter is the default choice unless review argues otherwise.
