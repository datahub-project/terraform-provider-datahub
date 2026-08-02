# Provider Roadmap

This document catalogs the DataHub API surface — OpenAPI REST + GraphQL — and classifies each area by relevance to the Terraform provider. It is the basis for deciding what to build next.

**Current provider state:** see [the docs index](index.md), or `internal/provider/provider.go` for the authoritative registration list.

This line used to enumerate every resource and data source. It drifted twice — it read "v0.7.0 + main" while the provider was on v0.19.1, twelve releases later, which caused a 2026-08-02 re-survey to re-raise six gaps that had already shipped. A hand-maintained inventory inside a planning document has no mechanism keeping it honest, so it is not worth having. The tier tables below carry shipped/unshipped state per candidate, and those are what this document is actually for.

## Scope principles

- **In scope:** platform-level configuration that engineering teams want pinned in code (taxonomy definitions, ingestion config, RBAC, structured-property schemas, governance tests, data contracts).
- **Out of scope:** per-asset metadata enrichment — tag/term/owner/description on individual datasets, charts, dashboards. DataHub's collaborative-catalog value depends on business users editing these in the UI; Terraform-managed enrichment overwrites their changes on every apply.
- **Read constraint:** all Read/ImportState paths must use the strongly-consistent OpenAPI v3 entity endpoint (`GET /openapi/v3/entity/{type}/{urn}`), never GraphQL `list*` queries (eventually-consistent OpenSearch).
- **Write constraint:** GraphQL mutations are preferred for writes (service-layer validation, encryption); OpenAPI write endpoints bypass service logic.

## Relevance ratings

| Rating | Meaning |
|---|---|
| **HIGH** | Slow-moving platform/governance configuration; classic IaC candidate; clear write+read path; deterministic URN; no per-asset enrichment risk. |
| **MEDIUM** | Plausible candidate with a real concern: Cloud-only with `internal` stability classification, list-aspect ownership complexity, newer/experimental feature, or overlap with a HIGH candidate. |
| **LOW** | Per-asset enrichment, operational/runtime endpoint, or a data-source-only candidate that doesn't justify a resource. |
| **IRRELEVANT** | Infrastructure/ops endpoints, deprecated v1 routes, AI/Compass/Files plumbing, Iceberg REST catalog, etc. Documented so nothing is silently dropped. |

---

## Category summary

| # | Category | Top HIGH candidates | TF shape | Notes |
|---|---|---|---|---|
| 1 | **Ingest** | `connection` | resource | Credentials normalization for ingestion sources. |
| 2 | **Governance taxonomy** | `domain`, `data_product`, `glossary_node`, `glossary_term`, `tag` (definitions), `ownership_type` | resource + data source | Largest HIGH bucket; pure config; not touched by ingestion. |
| 3 | **Schema metadata** | `structured_property`, `form` (definitions only) | resource + data source | Definitions are HIGH; *value assignments* to assets are deny-list. |
| 4 | **RBAC / Access** | `policy`, `corp_group`, `corp_group_member`, `role_assignment`, `corp_user`, `local_user_login` shipped (v0.4.0). Remaining: `service_account` (OSS 1.4.0+ and Cloud), `oauth_authorization_server` (Cloud). | resource + data source | High leverage for ops teams; some Cloud-only. |
| 5 | **Tests** | `metadata_test` | resource + data source | Declarative metadata quality rules. |
| 6 | **Observe / Assertions** | `data_contract` (Cloud), `assertion_assignment_rule` (Cloud) | resource | Per-asset assertions are MEDIUM-at-best; rules and contracts are the leverage points. |
| 7 | **Org settings** | `global_settings` (singleton) | resource | Singleton shape; low ceremony, high value. |
| 8 | **Per-asset enrichment** | *(deny-list)* | none | Explicitly out of scope — documented below. |
| 9 | **Action workflows** | *(MEDIUM, experimental)* | resource | Cloud-only governance workflows; revisit when stable. |
| 10 | **Lineage / versioning / ER** | *(LOW)* | data source only | Manual lineage overwritten by ingestion. |
| 11 | **AI / Compass / Documents / Subscriptions** | *(IRRELEVANT)* | none | Runtime/per-user/experimental Cloud features. |
| 12 | **Infrastructure / ops** | *(IRRELEVANT)* | none | Kafka, ES, K8s, RestoreIndices, Iceberg REST catalog, etc. |
| 13 | **Home-page layout (page templates/modules)** | `dataHubPageTemplate`, `dataHubPageModule` | resource | Split out of 11 -- `category: core` in OSS, not Cloud-only; `GLOBAL` scope is the org-default admins configure. |

---

## Category 1: Ingest

Current coverage: `datahub_ingestion_source`, `datahub_secret`, `datahub_remote_executor_pool`, `datahub_connection` (+ data sources including bulk-enumerate `datahub_ingestion_sources`, `datahub_secrets`, `datahub_connections`).

| Operation | Type | Relevance | Cloud-only | Notes |
|---|---|---|---|---|
| `createIngestionSource` / `updateIngestionSource` / `deleteIngestionSource` / `ingestionSource(urn)` | M/Q | covered | no | `datahub_ingestion_source`. Gap: provider uses OpenAPI for writes; GraphQL mutations exist — worth reviewing but not a new TF component. |
| `createSecret` / `updateSecret` / `deleteSecret` + OpenAPI Read | M | covered | no | `datahub_secret`. |
| `createRemoteExecutorPool` / `updateRemoteExecutorPool` / `getRemoteExecutorPool` + OpenAPI Delete | M/Q | covered | yes | `datahub_remote_executor_pool`; mutations classed `category: internal`. |
| `updateDefaultRemoteExecutorPool` + `defaultRemoteExecutorPool` | M/Q | covered | yes | Folded into `datahub_remote_executor_pool` as `is_default` since v0.2.0; a separate `datahub_default_remote_executor_pool` singleton was considered and rejected. The default is a single global pointer (the `dataHubRemoteExecutorPoolGlobalConfig` singleton aspect, whose only field is `defaultExecutorPoolId`) that `is_default` projects onto each pool; promoting one pool atomically demotes the prior default, and the demotion is strongly consistent (verified live 2026-07-03). Because that aspect carries no other fields, the flag is sufficient and a singleton resource would add no value. Provider uses `updateDefaultRemoteExecutorPool` (confirmed). |
| `upsertConnection` / `connection(urn)` / `deleteConnection` | M/Q | covered | yes+OSS | `datahub_connection` resource (v0.3.0). OSS delete falls back to OpenAPI DELETE (GraphQL mutation absent in OSS). |
| `getRemoteExecutor` (instance) | Q | LOW | yes | Read-only. Intended backing query for `datahub_ingestion_executor` data source. |
| `listIngestionSources` / `listSecrets` / `searchAcrossEntities (DATAHUB_CONNECTION)` | Q | covered | varies | `datahub_ingestion_sources`, `datahub_secrets`, `datahub_connections` bulk-enumerate data sources (v0.3.0). Eventually consistent - acceptable for enumeration, never used in Read/ImportState. |
| `getSecretValues` | Q | LOW | no | Decrypted secret readout — doesn't fit, `value` is WriteOnly in state. |
| `ingestionSourceForEntity` | Q | LOW | no | Reverse lookup — niche data source candidate. |
| `createIngestionExecutionRequest` / `cancelIngestionExecutionRequest` / `rollbackIngestion` / `createTestConnectionRequest` | M | IRRELEVANT | no | Runtime/operational. |
| `executionRequest(urn)` / `listExecutionRequests` / `getRateLimitInfo` / executor telemetry family | Q/M | IRRELEVANT | varies | Run telemetry. |

**`datahub_connection`:** shipped in v0.3.0 (PR #26). Reusable credential-bearing config referenced by ingestion sources. Works on both OSS and DataHub Cloud; OSS delete uses the OpenAPI endpoint since the GraphQL mutation is absent in OSS. URN key: user-supplied `id`.

---

## Category 2: Governance Taxonomy

The single largest HIGH bucket. All entities are slow-moving, governance/engineering-team-owned, and not touched by ingestion.

| Operation | Type | Relevance | Cloud-only | Notes |
|---|---|---|---|---|
| `createDomain` / `deleteDomain` / `moveDomain` + `domain(urn)` | M/Q | covered | no | `datahub_domain` resource + data source (v0.5.0, [PR #42](https://github.com/datahub-project/terraform-provider-datahub/pull/42)). User-supplied `domain_id` avoids UUID URN trap; reparenting via `moveDomain` mapped to `parent_domain` attribute. |
| `createDataProduct` / `updateDataProduct` / `deleteDataProduct` + `dataProduct(urn)` | M/Q | covered | no | `datahub_data_product` resource + data source + `datahub_data_products` bulk-enumerate data source ([PR #53](https://github.com/datahub-project/terraform-provider-datahub/pull/53)). User-supplied `data_product_id` avoids UUID URN trap; create/update write `dataProductProperties` and `domains` aspects via OpenAPI v3 (GraphQL mutations cannot set `external_url` or `custom_properties`). Asset membership is intentionally out of scope. |
| `createGlossaryNode` + `deleteGlossaryEntity` + `glossaryNode(urn)` | M/Q | covered | no | `datahub_glossary_node` resource + data source + `datahub_glossary_nodes` bulk-enumerate data source (v0.6.0, [PR #44](https://github.com/datahub-project/terraform-provider-datahub/pull/44)). User-supplied `node_id`; reparenting via `updateParentNode`; `domain` attribute via `setDomain`/`unsetDomain`. |
| `createGlossaryTerm` + `deleteGlossaryEntity` + `glossaryTerm(urn)` + scoped `updateName` / `updateDescription` / `updateParentNode` | M/Q | covered | no | `datahub_glossary_term` resource + data source + `datahub_glossary_terms` bulk-enumerate data source (v0.6.0, [PR #44](https://github.com/datahub-project/terraform-provider-datahub/pull/44)). User-supplied `term_id` (max 56 chars); reparenting in place; `domain` attribute. |
| `createTag` / `updateTag` / `deleteTag` / `setTagColor` + `tag(urn)` | M/Q | covered | no | `datahub_tag` resource + data source + `datahub_tags` bulk-enumerate data source (v0.7.0, [PR #48](https://github.com/datahub-project/terraform-provider-datahub/pull/48)). User-supplied `tag_id`; `color_hex` via `setTagColor`; renames via OpenAPI v3 tagProperties aspect (updateName does not support Tag). Tag assignments remain deny-list. |
| `createOwnershipType` / `updateOwnershipType` / `deleteOwnershipType` + OpenAPI entity | M/Q | covered | no | `datahub_ownership_type` resource + data source + `datahub_ownership_types` bulk-enumerate data source ([PR #50](https://github.com/datahub-project/terraform-provider-datahub/pull/50)). User-supplied `type_id`; create/update write via OpenAPI v3 aspect endpoint (GraphQL `createOwnershipType` mints a server-side UUID and is not used). `type_id` values beginning `__system__` rejected at plan time. |
| `addRelatedTerms` / `removeRelatedTerms` | M | MEDIUM | no | Possible `datahub_glossary_term_relationship` resource (typed: isA, hasA, contains, values, relatedTerm). Aspect-list ownership applies. |
| `createApplication` / `updateApplication` / `deleteApplication` + `application(urn)` | M/Q | MEDIUM | verify | Newer entity type; semantic overlap with `data_product` unclear. Confirmed present on Cloud. Defer until stable. |
| `relationshipType` entity (`RelationshipTypeInfo`) | entity | **WATCH** | Cloud today | Added to the Cloud fork 2026-07-18 alongside `StructuredPropertySettings.isRelationship` (see Category 3) — first-class user-defined relationship types, letting a structured property act as a graph edge. HIGH-shaped if it ships: a slow-moving, admin-owned taxonomy object, exactly this provider's remit. Not yet deployed — `RelationshipType` introspects as null on demo.acryl.io v2.0.3 (2026-08-02) — and absent from OSS entirely (no PDL package, no `entity-registry.yml` entry). Track the two together; neither is buildable until Cloud finishes shipping the pair. |
| `createGlossaryTermVersion` / versioning queries | M/Q | LOW | yes | Cloud-only temporal feature; not declarative. |
| `setDomain` / `unsetDomain` / `batchSetDomain` / `batchSet*Application` / `batchSet*DataProduct` | M | IRRELEVANT | no | Per-asset enrichment — deny-list. |
| `getRootGlossaryNodes` / `getRootGlossaryTerms` / `listDomains` / `listDataProductAssets` | Q | LOW | no | OpenSearch-backed; data-source-only with documented lag. |

**Glossary implementation notes (resolved in v0.6.0):**
- Property aspects (`glossaryNodeInfo`, `glossaryTermInfo`) are set via OpenAPI aspect PATCH after create, as planned.
- Shared `updateName`/`updateDescription` mutations are scoped to glossary URN types in the shared client helpers `UpdateEntityName` / `UpdateEntityDescription`.
- URN key is user-supplied `node_id` / `term_id` (not a hierarchical path or UUID), matching the deterministic pattern used across the provider.

---

## Category 3: Schema Metadata

| Operation | Type | Relevance | Cloud-only | Notes |
|---|---|---|---|---|
| `createStructuredProperty` / `updateStructuredProperty` / `deleteStructuredProperty` + `structuredProperty(urn)` | M/Q | covered | no | `datahub_structured_property` resource + data source + `datahub_structured_properties` bulk-enumerate data source (v0.7.0, [PR #49](https://github.com/datahub-project/terraform-provider-datahub/pull/49)). User-supplied `property_id`; `value_type`, `cardinality`, `entity_types`, `allowed_values`, `settings {}`. Additive-only update constraint (list fields + cardinality) forces replacement on removal. Value assignments to assets remain deny-list. |
| `StructuredPropertySettings.isRelationship` | aspect field | **WATCH — do not build yet** | Cloud today | New field on the existing `datahub_structured_property.settings {}`. Live on demo.acryl.io v2.0.3 (verified 2026-08-02); absent from OSS master `53064c2d` (2026-08-01, checked one day old). **Do not read that as a settled Cloud-only boundary.** It landed in the Cloud fork on 2026-07-18 as part of `feat(model): first-class relationshipType entity + structured-property graph edges` — a whole feature spanning `metadata-models`, `GraphService`, a graph-edge extractor, a new `relationshipType` entity and an ontology-graph UI. That is core metadata-model territory, the layer DataHub has historically upstreamed, and fifteen days of OSS absence is indistinguishable from "not upstreamed yet". It is also **half-deployed**: the field is live on Cloud but the `RelationshipType` GraphQL type is not (both probed 2026-08-02), so Cloud is running the toggle without the entity it toggles into. Building now buys churn. Re-probe both before promoting. When it does land, it needs OSS/Cloud-conditional emission — sending it unconditionally would break OSS users. |
| `createForm` / `updateForm` / `deleteForm` + `form(urn)` | M/Q | **HIGH** | no | **New:** `datahub_form` resource + data source. Metadata-collection forms (governance/compliance prompts). |
| `createDynamicFormAssignment` + `formInfo`/`dynamicFormAssignment` aspects | M | MEDIUM | verify | Declarative rule-based form assignment ("attach this form to all datasets in domain X"). Model as a nested attribute on `datahub_form` rather than a separate resource — assignment rules are a property of the form. |
| `createBusinessAttribute` / `updateBusinessAttribute` / `deleteBusinessAttribute` + `businessAttribute(urn)` | M/Q | MEDIUM | verify | Newer than structured properties; overlapping concept. Confirmed present on Cloud. Defer until patterns settle. |
| `upsertStructuredProperties` / `removeStructuredProperties` | M | **covered (partial)** | no | `datahub_structured_property_assignment` resource. **Not a deny-list exception — the deny-list rule applied correctly.** The rule targets ingestion-owned and business-user-owned *data assets*; it has never covered platform-config entities that this provider already manages. So assignment is supported for `domain`, `glossaryNode`, `glossaryTerm`, `dataProduct`, `corpuser` (including service accounts), `corpGroup` and `dataContract`, and **rejected** for datasets, charts, dashboards and everything else ingestion owns. This entry read `IRRELEVANT | deny-list` until 2026-08-02 and contradicted shipped code; the same correction applies to Category 8. |
| `batchAssignForm` / `batchRemoveForm` / `verifyForm` / `submitFormPrompt` / async batch variants | M | IRRELEVANT | mostly Cloud | Runtime form-filling — deny-list. |
| `addBusinessAttribute` / `removeBusinessAttribute` | M | IRRELEVANT | no | Per-asset assignment — deny-list. |
| `formAnalytics` / `formAnalyticsConfig` / `getFormsForActor` / `sendFormNotificationRequest` | Q/M | IRRELEVANT | yes | Telemetry/notifications. |

---

## Category 4: RBAC / Access / Identity

| Operation | Type | Relevance | Cloud-only | Notes |
|---|---|---|---|---|
| `createPolicy` / `updatePolicy` / `deletePolicy` + OpenAPI policy entity | M | covered | no | `datahub_policy` resource + `datahub_policies` data source (v0.4.0). PLATFORM and METADATA policy types; deterministic `policy_id`; full-state ownership of privileges/actors/resources lists. Both resource-scope forms are covered: the criteria `resources.filter` (tag/domain/container/multi-type scoping) and the deprecated `type`/`resources`/`all_resources`. `resources.privilegeConstraints` (sub-resource modification constraints, same `PolicyMatchFilter` shape as `filter`) is **not** yet modelled -- the provider owns the full `resources` map, so a constraint set outside Terraform is dropped on the next apply. |
| `createGroup` / `removeGroup` / `updateCorpGroupProperties` + `corpGroup(urn)` | M/Q | covered | no | `datahub_corp_group` resource + `datahub_corp_group` / `datahub_corp_groups` data sources (v0.4.0). |
| `addGroupMembers` / `removeGroupMembers` | M | covered | no | `datahub_corp_group_member` resource (v0.4.0). |
| `batchAssignRole` + `dataHubRole(urn)` | M | covered | no | `datahub_role_assignment` resource + `datahub_role` / `datahub_roles` data sources (v0.4.0). |
| `ingestProposal` (corpUserInfo aspects) + `removeUser` | M | covered | no | `datahub_corp_user` resource + data source (v0.4.0). Upsert semantics via OpenAPI v3. |
| `POST /auth/signUp` + `createNativeUserResetToken` | REST/M | covered | no | `datahub_local_user_login` resource (v0.4.0). Native-auth login provisioning; exposes single-use 24h reset URL. Works on both OSS and Cloud (see design doc for OSS vs Cloud signUp differences). |
| `createServiceAccount` / `deleteServiceAccount` + `getServiceAccount` | M/Q | **HIGH** | no (OSS 1.4.0+) | **New:** `datahub_service_account` resource. Upstreamed to OSS in DataHub Core v1.4.0 ([PR #15972](https://github.com/datahub-project/datahub/pull/15972), Jan 2026) - NOT Cloud-only. A service account is just a `corpUser` carrying a `subTypes = ["SERVICE_ACCOUNT"]` aspect under a `service_` URN prefix. `createServiceAccount` mints a UUID (`service_<uuid>`) but does nothing else - it batch-upserts three aspects (`corpUserKey`, `corpUserInfo`, `subTypes`). So, exactly like `ownership_type`/`domain`, the provider bypasses the UUID mutation and writes those aspects via OpenAPI v3 with a **user-supplied id** -> deterministic `urn:li:corpuser:service_<id>`. `createServiceAccount` returns only the account entity (no secret); the token is minted separately via the access-token flow (`createAccessToken`, write-once). See the note below. |
| `upsertOAuthAuthorizationServer` / `deleteOAuthAuthorizationServer` + `oauthAuthorizationServer(urn)` | M/Q | **HIGH** | yes | **New:** `datahub_oauth_authorization_server` resource. Config for external OAuth IdPs. Cleanest "config" object in the access space. |
| `updateServiceAccountDefaultView` | M | MEDIUM | no (OSS 1.4.0+) | Single attribute on `datahub_service_account`; also present in OSS `auth.graphql`. |
| `createAccessToken` / `revokeAccessToken` + `getAccessToken` | M/Q | MEDIUM | no | Write-once tokens with TTL; rotation = constant churn. Skip unless customers ask. |
| `service(urn)` / `listServices` | Q | MEDIUM | yes (queries only) | Cloud-only registered services — data source candidate. Narrower than it looks: the `service` and `aiAgent` **entity models** have been upstreamed to OSS, but the `service` / `listServices` **GraphQL queries** remain Cloud-only. The Cloud-only marking stands, for the read surface rather than the model. |
| `createInviteToken` / `sendUserInvitations` / `revokeUserInvitation` | M/Q | LOW | varies | Invite workflows; `datahub_local_user_login` handles the native-auth case. `sendUserInvitations` (Cloud-only per-user email invite) is a future follow-up. |
| `removeUser` / `updateUserStatus` / user-settings family | M | LOW/IRRELEVANT | no | Per-user state and UI prefs. |
| `resetLinkedIdentities` / `updateLinkedIdentities` | M | LOW | yes | SCIM/SSO identity linking; niche. |
| `listPolicies` / `listGroups` / `listUsers` / `listServiceAccounts` | Q | covered (partial) | varies | `datahub_policies`, `datahub_corp_groups` bulk-enumerate (v0.4.0). `listUsers` available via `datahub_corp_user` import enumeration. |
| SCIM REST endpoints | REST | LOW | no | Provisioning usually owned by IdP. |
| SCIM-Configuration / auth-service-controller | REST | IRRELEVANT | no | Auth bootstrap surfaces. |

**`datahub_service_account` note (revised 2026-07-05 after OSS verification):**
- **OSS + Cloud**, not Cloud-only. Available on DataHub Core >= v1.4.0 and Cloud >= v0.3.17. Verified in `datahub-graphql-core/src/main/resources/auth.graphql` (OSS core) plus OSS resolvers, `ServiceAccountService`, and OSS smoke tests. Below the version floor the endpoints are absent, so the resource needs a graceful "not available" diagnostic (a version-floor discriminator, like `datahub_connection`'s OSS/Cloud handling).
- **URN: use a deterministic aspect write, not the UUID mutation.** The GraphQL `createServiceAccount` mints `service_<uuid>` (non-deterministic), which would be the design-doc red flag - BUT the resolver does nothing special: it batch-upserts three corpUser aspects and returns. Verified in `CreateServiceAccountResolver.java`: `corpUserKey` (username), `corpUserInfo` (`active=true`, `displayName`, `title`=description), and `subTypes = ["SERVICE_ACCOUNT"]`. Recognition as a service account is purely that subtype (`ServiceAccountUtils.isServiceAccount` checks the `SubTypes` aspect), and `ServiceAccountUtils.buildServiceAccountUrn(name)` already defines the deterministic `service_<name>` form. So the provider takes a **user-supplied id** and writes those three aspects via OpenAPI v3 -> deterministic `urn:li:corpuser:service_<id>`, idempotent and import-clean, exactly as `ownership_type` and `domain` bypass their UUID-minting GraphQL creates. No service-layer logic is bypassed (the resolver has none beyond an auth check + UUID generation), so the "GraphQL-preferred" write rule does not apply here.
- **Stay in lane on the shared corpUser entity.** Read/Import must verify the `SERVICE_ACCOUNT` subtype and refuse to manage a plain corpUser (human user or ingested owner-reference). This is the guard that lets a service-account resource coexist safely with the overloaded corpUser entity - the clean machine-identity subset that the parked human-user work could not achieve.
- **Two-aspect shape, not a single write-once create.** `createServiceAccount` returns only the account entity (urn/name/description) - no secret. The write-once credential is the *token*, minted separately via `createAccessToken` (token type = service account, shown once). So the resource is really "idempotent account + optional write-once token" (the token as a separate write-once resource), not the single write-once-create originally assumed.
- **Coexistence caveat.** UI/GraphQL-created accounts are `service_<uuid>`; TF-created ones are `service_<id>`. They coexist fine - TF manages only its own deterministic ids. Importing a UI-made UUID account is possible but leaves the uuid as the id (adopt-computed). A live acceptance test should assert a TF-created account appears in `listServiceAccounts`/`getServiceAccount`, so any future drift in the required aspect set breaks CI rather than users.
- **Prerequisites:** Metadata Service Authentication enabled (`METADATA_SERVICE_AUTH_ENABLED=true` on gms + frontend), and the "Manage Service Accounts" platform privilege (Admin role carries it). No OAuth authenticator config is required for DataHub-issued tokens.

---

## Category 5: Tests / Metadata Tests

| Operation | Type | Relevance | Cloud-only | Notes |
|---|---|---|---|---|
| `createTest` / `updateTest` / `deleteTest` + `test(urn)` | M/Q | **HIGH** | yes (API) | **New:** `datahub_metadata_test` resource + data source. Declarative metadata-quality rules, e.g. "every PROD dataset must have an owner". GraphQL mutations confirmed present in OSS schema (`datahub-graphql-core`); generated frontend hooks exist but no management UI in OSS (Cloud-only). OSS results display on the dataset Governance tab requires `testsConfig.enabled = true` in app config (defaults to false). |
| `validateTest` | Q | MEDIUM | yes (API) | Useful for plan-time syntax validation of test definitions. No UI counterpart in OSS. |
| `runTests` / `runTestDefinition` | M | IRRELEVANT | varies | Operational. |

---

## Category 6: Observe (Assertions, Monitors, Data Contracts, Incidents)

| Operation | Type | Relevance | Cloud-only | Notes |
|---|---|---|---|---|
| `upsertDataContract` + read via `entity()` or OpenAPI | M | **HIGH** | yes | **New:** `datahub_data_contract` resource. Bundles freshness/schema/quality/SLA assertions on a dataset. **Concerns:** (1) no dedicated `dataContract(urn)` GraphQL query confirmed missing on live probe — Read must use OpenAPI v3; (2) no dedicated `deleteDataContract` mutation — investigate soft-delete via aspect removal. Implement after `datahub_assertion_assignment_rule`. |
| `createAssertionAssignmentRule` / `updateAssertionAssignmentRule` / `deleteAssertionAssignmentRule` + `assertionAssignmentRule(urn)` | M/Q | **HIGH** | yes | **New:** `datahub_assertion_assignment_rule` resource. Declarative rule-based assignment of assertions/monitors to entities matching a filter — much higher leverage than per-asset assertions. Ship this first. |
| `createSqlAssertion` / `createDatasetAssertion` / `createFieldAssertion` / `createFreshnessAssertion` / `createVolumeAssertion` / `updateDatasetAssertion` / `deleteAssertion` / `upsertCustomAssertion` | M | **covered** | mostly Cloud | Six resources shipped — `datahub_freshness_assertion`, `datahub_volume_assertion`, `datahub_sql_assertion`, `datahub_field_assertion`, `datahub_schema_assertion`, `datahub_custom_assertion` — plus `datahub_assertion` / `datahub_assertions` data sources. The "defer until rules + contracts are in place" sequencing was followed: rules ([PR #75](https://github.com/datahub-project/terraform-provider-datahub/pull/75)) and contracts ([PR #80](https://github.com/datahub-project/terraform-provider-datahub/pull/80)) landed first, and scope is limited to `NATIVE`-source assertions so the runtime/UI-driven lifecycle tension the row identified never arises. |
| `upsertDataset*AssertionMonitor` (5 variants) / `createAssertionMonitor` / `updateAssertionMonitorSettings` / `updateMonitorStatus` / `deleteMonitor` | M | MEDIUM | yes | Cloud-only monitor management. Defer. |
| `assertion(urn)` / `listAssertions` | Q | **covered** | mostly Cloud | `datahub_assertion` + `datahub_assertions` data sources. |
| `assertionInferenceAdjustmentRule` family | M/Q | MEDIUM | yes (`category: core`) | **New since the 2026-05-26 baseline.** Deterministic user-supplied `id`, so URN determinism is satisfied. GraphQL is read-only, so writes would go via OpenAPI v3 — the inverse of this provider's usual split, and worth confirming no service-layer logic is bypassed before relying on it. No UI and no create resolver yet. Tier 4 watch, with an explicit promotion trigger: **if it stays API-only, Terraform becomes the natural authoring surface** and this moves up rather than staying deferred. |
| Run/report/backfill family | M/Q | IRRELEVANT | yes | Runtime/operational. |
| `proposeDataContract` | M | LOW | yes | Governance flow. |
| Incident lifecycle | M | IRRELEVANT | yes | Operational. |

---

## Category 7: Org-level Settings (Singleton Resources)

Each setting is a singleton (URN `urn:li:globalSettings:0` etc.). Singleton shape: hard-code URN, no `Create` (just `Update`), treat as drift-correction. See `aws_default_*` for precedent.

**Granularity rule — group by administrative domain.** One resource per coherent administrative domain: the settings one persona owns, one privilege gates, one availability tier applies to. Aspect sections are the raw material; merge where a domain spans several. Do **not** anchor on the UI page (pages mix org-wide and per-user settings, span multiple aspects, and get rearranged) or on the GraphQL mutation (`updateGlobalSettings` alone spans SSO, notifications, integrations and four AI groups). Do **not** collapse everything into one `datahub_global_settings`: this provider's resources own what they declare, so a fat settings resource would reset SSO and AI config when a user manages only their logo — plus privileges, availability tiers and owning teams all differ per domain. Full reasoning, precedent and the already-decided consequences: `docs/design/provider-org-settings.md`.

Expected end state is roughly four settings resources, not ten. **Check the scope of every control before modelling a settings page** — every DataHub settings page examined so far mixes org-wide and per-user settings, and per-user is out of scope.

**Small-singleton pattern — ESTABLISHED by `datahub_organization_display_preferences`** (shipped 2026-07-28; see `docs/design/provider-org-settings.md` for the pattern as implemented and the empirical findings behind it). It manages `globalSettingsInfo.visual.customOrgName` / `.customLogoUrl` — the org name and logo on the UI's Settings -> Preferences page — and is the reference implementation for the remaining settings singletons. `datahub_global_settings` was the originally-planned first instance; the display-preferences slice landed first because it was what a real request asked for, and it turned out to be a cleaner pattern-setter (a coherent two-field section, entirely Cloud-only, so no mixed-availability complications).

Several system-level singletons remain on the horizon (the rest of this category, plus the remote-executor global config at `urn:li:dataHubRemoteExecutorGlobalConfig:primary`). Reuse the established pattern rather than hand-rolling each. Conventions:

- **Hard-coded URN, no user-facing `id`.** The singleton always conceptually exists.
- **Update-only lifecycle.** No real `Create` (Create = read-current-then-apply-desired); `Delete` = reset-to-backend-default or no-op, *not* entity deletion. Document which, per resource.
- **Read via the OpenAPI v3 entity endpoint** on the singleton URN (strongly consistent), never a `list*`/search query.
- **Handle nullable fields honestly.** Some backends offer no mutation to *clear* a field (only to set it) -- e.g. the executor default pointer can be set via GraphQL but only cleared by deleting the singleton aspect via OpenAPI v3. Where a field models "unset", verify a clear path exists before modelling it as removable.
- **Not every singleton needs its own resource.** The executor default pointer is a one-field singleton already projected onto `datahub_remote_executor_pool.is_default`; a dedicated resource would add no value. Prefer a singleton resource when the aspect carries multiple coherent fields, or when a flag-on-member would create a cross-instance ("conch") invariant that Terraform cannot enforce at plan time.

| Operation | Type | Relevance | Cloud-only | Notes |
|---|---|---|---|---|
| `updateGlobalSettings` / `globalSettings` | M/Q | **HIGH** | yes | **Not one resource.** This single mutation spans SSO, notifications, integrations and four AI groups, so it is a transport, not a domain — split per the granularity rule above. Also note this whole surface is Cloud-only (OSS exposes no `globalSettings` query or `updateGlobalSettings` mutation at all) and the entity is `category: internal`. |
| AI settings (`aiAssistant`, `documentationAi`, `aiContext`, `mcpSettings`) | M/Q | **HIGH** | yes | **New:** one `datahub_ai_settings` covering all four — one persona, one page, one tier. `aiPlugins`/`mcpServers` are object lists with independent lifecycles, so they are child resources (e.g. `datahub_ai_plugin`), not attributes. Per-user siblings (`updateCorpUserAiAssistantSettings`, `updateUserAiPluginSettings`, `updateUserContextDocumentsSettings`) are out of scope. |
| MCP servers (`mcpSettings` / `mcpServers` / `defaultMcpServerOverrides`) | M/Q | MEDIUM | yes | **New since the 2026-05-26 baseline**, live on `GlobalSettings` / `UpdateGlobalSettingsInput`. Superficially attractive — `McpServerConfigInput` carries a user-supplied `slug`, so URNs would be deterministic. **BLOCKED on shape churn, same treatment as the brand-colour row.** Two specific blockers: (1) the shape already differs structurally between deployed v2.0.3 and fork HEAD (fields relocated into `McpSettings`, a master `enabled` switch added), so anything built today targets a moving object; (2) `UpdateGlobalSettingsInput.servers` is **merge-by-slug**, which is the direct opposite of this provider's full-list-ownership rule — a resource owning the list could not remove a server, and one owning a single entry could not detect drift. Resolve the semantics before designing, not during. Belongs with the AI-settings domain by the granularity rule. |
| `ssoSettings` | M/Q | **excluded** | yes | Argued against: Terraform managing the mechanism that authenticates Terraform is a lockout risk, and recovery is manual regardless. |
| `updateAssetSettings` | M | **HIGH** | yes | **New:** `datahub_asset_settings` resource. |
| `updateDocPropagationSettings` / `docPropagationSettings` | M/Q | MEDIUM | no | Doc propagation feature toggle. |
| `updateContextGenerationSettings` / `contextGenerationSettings` | M/Q | MEDIUM | yes | Ingestion context config. |
| `updateApplicationsSettings` | M | MEDIUM | yes | Applications feature toggle. |
| `updateGlobalViewsSettings` / `globalViewsSettings` | M/Q | MEDIUM | no | Org default view setting. |
| ~~`updateOrganizationDisplayPreferences`~~ | M | **HIGH** | yes | **Shipped**: `datahub_organization_display_preferences` (resource + data source). Re-rated from LOW — org branding is exactly the slow-moving platform config this provider exists for, and the original "UI prefs" grouping conflated it with per-user preferences. |
| Brand colour (fork PR 11277) | M/Q | MEDIUM | yes | **BLOCKED until the Cloud release ships and reaches demo.acryl.io.** Announced 2026-07-31 for the *next* Cloud release, not the current one, and SaaS-only: one colour pick in Settings that the whole app adopts, built on the semantic-token work. **Folds into `datahub_organization_display_preferences`** as one more attribute, by the domain-grouping rule and by the precedent already recorded for org-wide dark/light mode — same aspect section, same persona, same privilege, same Cloud-only tier. Do **not** mint a `datahub_brand_color`. Nothing to design until the API surface is visible on demo: the mutation name, whether the value lands in `globalSettingsInfo.visual` alongside `customOrgName`/`customLogoUrl`, and the accepted colour format are all unknown from the announcement alone. Re-check when demo picks up the release. |
| `updateHelpLink` / `helpLink` | M/Q | MEDIUM | yes | **Folds into `datahub_organization_display_preferences`** as extra attributes — same aspect section (`globalSettingsInfo.visual`), same privilege tier, same persona. Deliberately *not* a separate `datahub_help_link`; doing so is what the domain-grouping rule exists to prevent. Also removes that resource's only naming imprecision (it would then cover `visual`, not two of its four fields). |
| `updateSampleDataSettings` | M | **excluded** | yes | Org-wide but not a pure config write: toggling asynchronously soft-deletes/restores sample-data entities via `SampleDataService`, so declarative apply/destroy is destructive. Trial-instance-only in the UI. Rationale in `docs/design/provider-org-settings.md`. |
| `updateApplicationsSettings` (Beta features) | M | **excluded** | no (OSS) | Org-wide and OSS-stable, so technically clean, but excluded as a product decision (2026-07-28): feature-preview flags are not this provider's job. Deliberately not backlogged. |
| `updateCorpUserLocaleSettings` and other per-user settings | M | IRRELEVANT | no | Per-user preferences; excluded by the provider's scope rule. |

**Caveat added 2026-07-28**: the "Cloud-only" column above answers "does the mutation exist on OSS", but the `globalSettings` entity itself is marked `category: internal` in `entity-registry.yml` — the same stability tier as `datahub_remote_executor_pool`'s underlying mutations (`category: internal` in DataHub Cloud, no external API stability guarantee). Treat `datahub_global_settings` under the same disclaimer convention when it's built, not as a plain-stable resource just because it's OSS-available. See Category 13 for the `homePage` sub-field specifically, which additionally lacks any public update mutation today.

---

## Category 8: Per-asset Enrichment (DENY-LIST)

**Do not implement these as TF resources.** They enrich individual data assets (datasets, charts, dashboards, ML models) whose metadata is owned by ingestion or business users. Managing them in Terraform creates an apply-overwrites-UI loop that erodes catalog trust.

**Tag assignments:** `addTag`, `addTags`, `batchAddTags`, `removeTag`, `batchRemoveTags`
**Term assignments:** `addTerm`, `addTerms`, `batchAddTerms`, `removeTerm`, `batchRemoveTerms`
**Owner assignments:** `addOwner`, `addOwners`, `batchAddOwners`, `removeOwner`, `batchRemoveOwners`
**Domain assignments:** `setDomain`, `unsetDomain`, `batchSetDomain`
**Data product assignments:** `batchSetDataProduct`, `batchAddToDataProducts`, `batchRemoveFromDataProducts`
**Application assignments:** `batchSetApplication`, `batchUnsetApplication`
**Structured-property values on assets:** `upsertStructuredProperties`, `removeStructuredProperties` — *on data assets only.* The provider ships `datahub_structured_property_assignment` for the config entities it already manages; see the Caveat below and the Category 3 row.
**Form assignments on assets:** `batchAssignForm`, `batchRemoveForm`, `refreshFormAssignment`, and all `verifyForm`/`submitFormPrompt`/async variants
**Business-attribute assignments:** `addBusinessAttribute`, `removeBusinessAttribute`
**Asset descriptions/names:** `updateDescription`, `updateShortDescription`, `updateName` (when applied to data assets), `updateDisplayProperties`, `updateDeprecation`, `batchUpdateDeprecation`, `batchUpdateSoftDeleted`, `updateEmbed`
**Per-type asset edits:** `updateDataset`, `updateDatasets`, `updateChart`, `updateDashboard`, `updateDataFlow`, `updateDataJob`, `updateNotebook`
**Institutional-memory links:** `addLink`, `removeLink`, `updateLink`, `upsertLink`
**Manual lineage edits:** `updateLineage`
**Generic passthrough:** `patchEntity`, `patchEntities`, `deleteReferences`, `reportOperation`

**Caveat:** setting `ownership`, `domains` or **structured-property values** *on a config entity that the provider manages* (e.g. the domain itself, a glossary term, a data product) is legitimate — the deny-list applies to these aspects on *data assets* (datasets, charts, etc.).

This is the whole of the rule, and it is worth stating positively because reading the list above as "these mutations are banned" gets it wrong: the deny-list is scoped by **who owns the entity**, not by which mutation writes it. `upsertStructuredProperties` against `urn:li:domain:finance` is platform configuration; the identical call against `urn:li:dataset:...` is enrichment that apply would overwrite. `datahub_structured_property_assignment` enforces exactly that split in code, accepting seven config entity types and rejecting the rest.

**Runtime/operational deny-list (also out of scope):** `runAssertion(s)`, `runAssertionsForAsset`, `testAssertion`, `reportAssertionResult`, `runTests`, `runTestDefinition`, `createIngestionExecutionRequest`, `cancelIngestionExecutionRequest`, `rollbackIngestion`, action-pipeline lifecycle mutations, incident lifecycle mutations, `bulkUpdateAnomalies`, `reportAnomalyFeedback`, `retryMonitorBackfill`, proposal accept/reject/propose mutations, all `*Notification*` mutations.

---

## Category 9: Action Workflows / Pipelines / Proposals (EXPERIMENTAL Cloud-only)

| Operation | Type | Relevance | Cloud-only | Notes |
|---|---|---|---|---|
| `upsertActionWorkflow` / `deleteActionWorkflow` / `listActionWorkflows` | M/Q | MEDIUM | yes | Governance/approval workflows. Configuration-flavored but early. Confirmed present on this Cloud instance. Revisit when stable. |
| `createActionPipeline` / `upsertActionPipeline` / `deleteActionPipeline` / `bootstrapActionPipeline` | M | **covered** | yes | `datahub_action_pipeline` resource + `datahub_action_pipelines` data source. Shipped despite the "defer" verdict because a concrete request arrived; the experimental/Cloud-internal caveat still applies and is recorded in `CLAUDE.md`'s Cloud-only table. |
| Pipeline lifecycle / proposal handling | M/Q | IRRELEVANT | yes | Operational. |

---

## Category 10: Lineage / Versioning / ER (LOW)

| Operation | Type | Relevance | Cloud-only | Notes |
|---|---|---|---|---|
| `updateLineage` | M | LOW | no | Manual lineage typically overwritten by ingestion. |
| `searchAcrossLineage` / `scrollAcrossLineage` | Q | LOW | no | Data-source-only for asset-discovery flows. |
| `linkAssetVersion` / `unlinkAssetVersion` / `versionSet` | M/Q | LOW | yes | Asset versioning — operational. |
| `createERModelRelationship` / `updateERModelRelationship` / `deleteERModelRelationship` + `erModelRelationship(urn)` | M/Q | LOW | no | Typically ingestion-discovered. |
| `createQuery` / `updateQuery` / `deleteQuery` | M | LOW | no | SQL query catalog — ingestion-discovered. |
| `setLifecycleStage` / `setLogicalParent` / `createTermConstraint` | M | LOW | yes | Experimental Cloud governance. |
| `shareEntity` / `unshareEntity` | M | LOW | yes | Cross-instance sharing; niche. |

---

## Category 11: AI / Compass / Documents / Subscriptions (IRRELEVANT)

All Cloud-only, runtime/per-user, or experimental. No TF resource shape:

- `dataHubAiConversation` family, AI memory, `inferDocumentation` — AI assistant
- `dataHubFile` family, `getPresignedUploadUrl` — file uploads
- `document` family, `searchDocuments` — wiki-style docs
- `subscription` family — per-user notifications
- `createAgent`/`updateAgent`/`deleteAgent`/`createTask`/`runTask` — AI agents (experimental)
- `createEval`/`runEvals` — AI evals (experimental)
- `upsertAiPlugin`/`deleteAiPlugin` — AI plugin config (borderline; revisit if customers ask). **Confirmed live** on demo.acryl.io v2.0.3 (2026-08-02) rather than assumed present; classification unchanged.
- `createDraftEntity` — governance drafts

`upsertPageModule`/`deletePageModule`/`upsertPageTemplate`/`deletePageTemplate` (UI page customization) previously sat in this bucket. Moved out to Category 13 (2026-07-28): verified directly against the OSS repo (not just schema surface) that these are `category: core` entities with a real `GLOBAL` org-default scope, not a Cloud-only or purely per-user mechanism -- see Category 13.

---

## Category 12: Infrastructure / Operations / Iceberg (IRRELEVANT)

No TF resource shape. Listed so nothing is silently excluded:

- **Kafka admin** (17 REST paths) — topic/consumer-group admin, MCP replay
- **Kubernetes ops** (18 REST paths) — pod scaling, configmap updates, cronjob triggers
- **ElasticSearch debug/ops/raw** (12 REST paths) — index inspection, raw doc fetches
- **RestoreIndices** (2), **HealthCheck** (7+1), **System Information** (4), **Maintenance Window** (3), **Async Write Tracing** (1), **GMS Throttle / API Requests** (2), **Events** (2) — infra/admin
- **OpenLineage** (1) — use ingestion sources instead
- **Iceberg REST catalog** (6 controllers, ~22 paths) — DataHub-as-Iceberg-catalog is a separate product surface
- **Entity Registry / Lineage Registry / Entity Consistency / Timeline / Logical Models** — internal/legacy/operational
- **Generic Relationships / Generic Timeseries / Relationships (v1) / Entities (v1) / Platform Entities** — legacy endpoints superseded by v2/v3
- *Added at the 2026-08-02 re-survey, all IRRELEVANT, listed so nothing is silently dropped:* **messaging operations** (`/openapi/operations/messaging/*`, 6), **rate limits** (`/openapi/v1/rate-limits/*`, 2), **billing** (`/openapi/v1/billing/*`, 2), plus support-bundle, entity-counts and `/openapi/v3/lineage` controllers present in the fork but not yet deployed to demo.acryl.io

---

## Category 13: Home-page Layout (Page Templates / Modules)

Split out of Category 11 (2026-07-28) after a closer look showed the earlier "Cloud-only, runtime/per-user, or experimental" bucketing didn't hold for the `GLOBAL`-scope half of this feature. Verified directly against the OSS repo, using the same two-layer approach as the "Stability classification" note below (entity-model layer + GraphQL/resolver layer), not just a schema-surface grep:

- **Entity-model layer**: `metadata-models/src/main/resources/entity-registry.yml` marks both `dataHubPageTemplate` and `dataHubPageModule` `category: core` (the same file marks `globalSettings` `category: internal` a few hundred lines earlier — direct evidence the registry does distinguish stability tiers, so a `core` marking here is meaningful, not a default).
- **GraphQL/resolver layer**: `upsertPageTemplate`/`deletePageTemplate`/`upsertPageModule`/`deletePageModule` and their resolvers (`UpsertPageTemplateResolver`, `DeletePageTemplateResolver`, `UpsertPageModuleResolver`, `DeletePageModuleResolver`) live in `datahub-graphql-core` — the OSS-stable location per this doc's own classification scheme — with matching unit tests, and the schema is consumed by `datahub-web-react` (the OSS frontend), not just declared and unused. The resolvers gate on a plain `AuthorizationUtils.canManageHomePageTemplates` privilege check when `scope == GLOBAL`; no license/feature-flag gate of the kind that would mark it Cloud-only.
- Confirmed identical in `datahub-fork` — no Cloud-only override or extension of this specific feature.

**The shape**: `dataHubPageTemplate` holds ordered rows of `dataHubPageModule` references (a `Form`/`metadata_test`-style nested-list resource, not a flat singleton). Both carry `visibility.scope` of `PERSONAL` or `GLOBAL`. `PERSONAL` templates are exactly the per-user, low-value case Category 11 correctly excludes. `GLOBAL` templates are the org-default home-page layout an admin configures — a legitimate platform-config resource under this provider's existing scope rules. See Tier 2 for the proposed `datahub_page_template` + `datahub_page_module` pair.

A related but distinct piece stays out of scope for now: `globalSettings.homePage.defaultTemplate` (the pointer saying "this `GLOBAL` template is the one everyone sees") lives on the `globalSettings` singleton from Category 7, which is `category: internal` and — as of this check — has only a read resolver (`GlobalHomePageSettingsResolver`), no public update mutation. Revisit once Category 7's `datahub_global_settings` work looks at that singleton in detail.

---

## Cross-cutting implementation notes

### Provider-level defaults (shipped)

The provider supports `default_tags`-style automatic labelling (`defaults` block plus the on-by-default `managed-by` auto-property; see the "Provider-level defaults" guide). Coverage is bounded by the entity registry: entity types that register no label aspects (`dataHubIngestionSource`, `dataHubSecret`, `dataHubPolicy`, `dataHubConnection`, ...) are documented no-ops. Extending the registry upstream (structuredProperties on ingestion sources et al.) is tracked outside this repo; when a server ships it, the defaults engine picks the type up by adding a matrix row and an assignment-target entry.

### Stability classification

GraphQL has no inline `category: internal` markers. Stability is inferred from schema file location:

- **OSS-stable:** file present in `~/src/datahub/datahub-graphql-core/src/main/resources/`
- **Cloud-stable:** `*.saas.graphql` / `*.acryl.graphql` in `~/src/datahub-fork`
- **Cloud-experimental:** file only in fork, no OSS equivalent — includes `aiPlugin`, `agents`, `aimemory`, `evals`, `share`, `draft`, `actions`, `actions_pipeline`, `ai`, `constraints`, `versioning.glossary`

Resources in the experimental bucket carry no external stability guarantee. Apply the same disclaimer convention as `datahub_remote_executor_pool`.

**Source location is necessary but not sufficient — check what the instance actually deploys.** The 2026-08-02 re-survey found two cases where the fork contains a feature that Cloud has not finished shipping (`relationshipType`, and the not-yet-deployed support-bundle / entity-counts / `/openapi/v3/lineage` controllers), and one where the fork has restructured a surface that is already live in an older shape (MCP servers). A file's presence in the fork tells you a feature exists somewhere; it does not tell you it is deployed, stable, or shaped the way trunk shows it. Live GraphQL introspection against demo.acryl.io settles the question in one query and works today — see the corrected note under "Survey methodology".

### URN determinism (per design doc requirement)

Before implementing any new resource, confirm the URN key strategy and that it matches the DataHub Python SDK convention:

| Resource | URN key source | Concern |
|---|---|---|
| `datahub_connection` | user-supplied `id` | verify SDK convention |
| `datahub_policy` | user-supplied `id` | UI creates UUID; provider must require explicit `id` |
| `datahub_corp_group` | user-supplied `id` | confirm SDK convention |
| `datahub_data_product` | user-supplied `id` | confirmed -- matches SDK `make_data_product_urn(id)` convention |
| `datahub_domain` | user-supplied `id` | UI creates UUID; provider must require explicit `id` |
| `datahub_glossary_node` / `datahub_glossary_term` | hierarchical path or UUID? | confirm SDK convention before implementing |
| `datahub_tag` | user-supplied `name` | confirm normalization |
| `datahub_ownership_type` | user-supplied `id` | confirmed -- matches SDK `make_ownership_type_urn(id)` convention |
| `datahub_structured_property` | user-supplied `id` | confirm |
| `datahub_form` | user-supplied `id` | confirm |
| `datahub_metadata_test` | user-supplied `id` | confirm |
| `datahub_data_contract` | derived from dataset URN | one contract per dataset |
| `datahub_assertion_assignment_rule` | user-supplied `id` | confirm |
| `datahub_service_account` | user-supplied `id` -> `service_<id>` | Deterministic via OpenAPI v3 aspect write (`corpUserKey`+`corpUserInfo`+`subTypes=[SERVICE_ACCOUNT]`), bypassing the UUID-minting `createServiceAccount` - same approach as `ownership_type`/`domain`. Read/Import subtype-guarded. OSS 1.4.0+ (not Cloud-only). |
| `datahub_oauth_authorization_server` | user-supplied `id` | Cloud-only; confirm |

### Aspect-list ownership

These resources contain list-type aspects — they must POST the complete desired list on every apply (per design doc § Upsert Semantics):

- `datahub_data_product` (`outputPorts`, `domains`)
- `datahub_policy` (`privileges`, `resources`, `actors`)
- `datahub_corp_group` membership (or split to `_member` resource)
- `datahub_glossary_term` (related-terms list, if folded in)
- `datahub_form` (prompts list)
- `datahub_metadata_test` (rules definition)
- `datahub_data_contract` (assertions list)
- `datahub_assertion_assignment_rule` (target filter)

### Read path per resource

Every Read/ImportState must use:
```
GET /openapi/v3/entity/{lowercase-urn-type}/{urn}
```

Lowercase URN types for each candidate: `datahubconnection`, `datahubpolicy`, `corpgroup`, `dataproduct`, `domain`, `glossarynode`, `glossaryterm`, `tag`, `ownershiptype`, `structuredproperty`, `form`, `test`, `datacontract`, `assertionassignmentrule`. For `serviceaccount` and `oauthauthorizationserver`: verify endpoint registration separately.

### OSS verification checklist (before claiming OSS badge)

1. Confirm the GraphQL mutation is in `repos/datahub/datahub-graphql-core/src/main/resources/` (OSS).
2. Confirm the OpenAPI v3 entity endpoint accepts the URN type against a local OSS Quickstart.
3. If both pass: `ossAndCloudBadge`. Otherwise: `cloudOnlyBadge`.

Needs verification before committing: `connection`, `structuredProperty`, `form`, `test`, `policy`, `corpGroup`. Schema files exist in OSS for most; API surface on a live OSS instance has not been verified.

---

## Candidate shortlist by tier

Ranked by leverage-to-effort. Each item is explicitly marked as a **TF resource**, **TF data source**, or **both**.

### Tier 1 — high leverage, well-understood pattern

| # | Terraform component | Type | OSS | Key concern |
|---|---|---|---|---|
| ~~1~~ | ~~`datahub_connection`~~ | resource | yes+OSS | **Shipped v0.3.0** ([PR #26](https://github.com/datahub-project/terraform-provider-datahub/pull/26)) |
| ~~2~~ | ~~`datahub_domain`~~ | resource + data source | yes | **Shipped v0.5.0** ([PR #42](https://github.com/datahub-project/terraform-provider-datahub/pull/42)) |
| ~~3~~ | ~~`datahub_tag`~~ | resource + data source | yes | **Shipped v0.7.0** ([PR #48](https://github.com/datahub-project/terraform-provider-datahub/pull/48)) |
| ~~4~~ | ~~`datahub_glossary_node`~~ | resource + data source | yes | **Shipped v0.6.0** ([PR #44](https://github.com/datahub-project/terraform-provider-datahub/pull/44)) |
| ~~5~~ | ~~`datahub_glossary_term`~~ | resource + data source | yes | **Shipped v0.6.0** ([PR #44](https://github.com/datahub-project/terraform-provider-datahub/pull/44)) |
| ~~6~~ | ~~`datahub_structured_property`~~ | resource + data source | yes | **Shipped v0.7.0** ([PR #49](https://github.com/datahub-project/terraform-provider-datahub/pull/49)) |

### Tier 2 — high leverage, larger design effort

| # | Terraform component | Type | OSS | Key concern |
|---|---|---|---|---|
| ~~7~~ | ~~`datahub_data_product`~~ | resource + data source | yes | **Shipped ([PR #53](https://github.com/datahub-project/terraform-provider-datahub/pull/53))** |
| ~~8~~ | ~~`datahub_policy`~~ | resource + data source | yes | **Shipped v0.4.0** |
| 9 | `datahub_form` | resource + data source | yes | Prompts list; `dynamicFormAssignment` as nested attr |
| 10 | `datahub_metadata_test` | resource + data source | yes (API) | API mutations confirmed OSS; management UI is Cloud-only; no nav entry in OSS frontend |
| ~~14~~ | ~~`datahub_service_account`~~ | resource + data source | yes (1.4.0+) | **Shipped ([PR #72](https://github.com/datahub-project/terraform-provider-datahub/pull/72))**, as designed below. Moved from Tier 3 - OSS since Core v1.4.0. Deterministic aspect write (`service_<id>` + `subTypes=[SERVICE_ACCOUNT]`) like `ownership_type`/`domain`, subtype-guarded read; token is a separate write-once resource; needs Metadata Service Auth enabled |
| ~~11~~ | ~~`datahub_ownership_type`~~ | resource + data source | yes | **Shipped ([PR #50](https://github.com/datahub-project/terraform-provider-datahub/pull/50))** |
| 17 | `datahub_page_template` + `datahub_page_module` | resource | yes | See Category 13. Template = ordered rows of module references (`form`/`metadata_test`-style nesting); `GLOBAL`-scope only (reject/ignore `PERSONAL` at plan time, same call as excluding per-asset enrichment); admin-gated via `manageHomePageTemplates`. |

### Tier 3 — Cloud-only, high leverage, accept stability caveat

| # | Terraform component | Type | Cloud | Key concern |
|---|---|---|---|---|
| ~~12~~ | ~~`datahub_assertion_assignment_rule`~~ | resource + data source | yes | **Shipped ([PR #75](https://github.com/datahub-project/terraform-provider-datahub/pull/75))**. The "ship before `data_contract`" sequencing was followed. |
| ~~13~~ | ~~`datahub_data_contract`~~ | resource + data source | yes | **Shipped ([PR #80](https://github.com/datahub-project/terraform-provider-datahub/pull/80))**. Both listed concerns held and were resolved as anticipated: Read uses the OpenAPI v3 entity endpoint because no `dataContract(urn)` query exists, and delete goes via aspect removal. |
| ~~14~~ | ~~`datahub_service_account`~~ | resource | ~~yes~~ | **Moved to Tier 2 - OSS since Core v1.4.0, not Cloud-only** (see revised note in Category 4) |
| 15 | `datahub_oauth_authorization_server` | resource | yes | Stable config object |
| ~~16~~ | ~~`datahub_corp_group` + `datahub_corp_group_member`~~ | resource + data source | yes | **Shipped v0.4.0** |

### Tier 4 — defer or revisit

- `datahub_business_attribute` — overlaps with `structured_property`; revisit once patterns settle
- `datahub_application` — overlaps with `data_product`; defer until entity stabilizes
- `datahub_action_workflow` / `datahub_action_pipeline` — experimental Cloud; revisit when stable
- `datahub_global_settings` / settings family — singleton resources; low-ceremony add-ons
- `datahub_view` (global views only) — niche
- `datahub_post` (announcements) — niche
- ~~`datahub_corp_user` (data source only)~~ — **shipped as resource + data source** in v0.4.0, not the data-source-only shape assumed here

### Already open

- `datahub_ingestion_executor` (data source, Cloud-only) — Vikunja #404841, backed by `getRemoteExecutor`

---

## Out of scope for this catalog

- Recipe document builder — not driven by an API endpoint; separate design discussion.
- Provider configuration enhancements (auth modes, retry/backoff, timeouts).
- OSS Quickstart vs Cloud capability detection — separate concern (see Bart Bot thread re: capability discriminator).
- Custom plan-time validators not backed by a TF resource.

---

## Survey methodology

Produced 2026-05-26. **Re-surveyed 2026-08-02** against demo.acryl.io (DataHub Cloud v2.0.3) via live GraphQL introspection and the OpenAPI group index: **+6 queries, +5 mutations, 0 removals, 0 renames, 1 new entity type addressable at `/openapi/v3/entity/`.** Every addition is Cloud-only and lands in a category already rated IRRELEVANT or MEDIUM-defer, so 68 days produced no new HIGH candidate. The re-survey's value was in what it found wrong about the *method* below, not in drift.

Sources:

1. **OpenAPI v2 REST** — `api-docs.json` (382 paths, 111 tags). ~~Note: the spec is mostly v2; the v3 namespace is very small.~~ **Wrong, corrected 2026-08-02.** The default `api-docs` document is an aggregate that exposes only 5 `/openapi/v3/*` paths. The real v3 surface lives in a separate springdoc group and is **1,007 paths / 4,322 operations across 87 entity types** (`GET /openapi/v3/api-docs/openapi-v3`, verified live). That is the surface this provider mandates for every Read and ImportState path, so the original note understated the most important namespace by roughly two orders of magnitude. Enumerate the groups first next time — `GET /openapi/v3/api-docs/swagger-config` lists them: `1. DataHub v3 (OpenAPI)`, `2. Events`, `3. OpenLineage`, `4. Operations`, `5. DataHub v2 (OpenAPI)`, `6. DataHub v1 (OpenAPI)`, `Metadata Tests`.
2. **GraphQL** — ~~live introspection is gated on DataHub Cloud instances;~~ **also wrong: full introspection works on demo.acryl.io** (verified 2026-08-02, and used for the org-settings design in July). The baseline schema was enumerated from the DataHub GraphQL source tree (OSS + Cloud) instead: 286 mutations + 157 queries. A live introspection query is now the cheapest way to re-run this audit, and the only way to see what a given instance actually deploys as opposed to what trunk contains.
3. **Live Cloud probes** — confirmed present on user's Cloud instance: `assertion`, `application`, `businessAttribute`, `connection`, `structuredProperty`, `test`, `form`, `document`, `role`, `listOAuthAuthorizationServers`, `listServiceAccounts`, `listActionWorkflows`, `listAssertionAssignmentRules`. Confirmed missing as top-level GraphQL query field: `dataContract` (use `entity()` or OpenAPI v3 instead).
4. **Re-probe 2026-08-02** — the following were re-checked live and are **still accurate**, recorded so a future reader does not re-litigate them: no `dataContract` query and no `deleteDataContract` mutation; the brand-colour field is still absent (BLOCKED note stands); `globalSettings` is still `category: internal`; `dataHubPageTemplate` / `dataHubPageModule` are still `category: core`; `oauthAuthorizationServer` is still Cloud-only.

**Known gap in the re-survey:** no archived copy of the 2026-05-26 OpenAPI spec exists on disk, so of the +39 REST paths observed, 14 were attributed to specific new controllers and **25 were not** — some of that residue is likely version skew between the baseline instance and fork source rather than genuinely new API. Archive the spec JSON alongside this document next time so the delta can be computed rather than reconstructed.
