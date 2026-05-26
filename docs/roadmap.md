# Provider Roadmap

This document catalogs the DataHub API surface — OpenAPI REST + GraphQL — and classifies each area by relevance to the Terraform provider. It is the basis for deciding what to build next.

**Current provider state (v0.2.0):** `datahub_ingestion_source` (resource + data source), `datahub_secret` (resource), `datahub_remote_executor_pool` (resource + data source), `datahub_me` (data source).

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
| 4 | **RBAC / Access** | `policy`, `corp_group`, `service_account` (Cloud), `oauth_authorization_server` (Cloud) | resource + data source | High leverage for ops teams; some Cloud-only. |
| 5 | **Tests** | `metadata_test` | resource + data source | Declarative metadata quality rules. |
| 6 | **Observe / Assertions** | `data_contract` (Cloud), `assertion_assignment_rule` (Cloud) | resource | Per-asset assertions are MEDIUM-at-best; rules and contracts are the leverage points. |
| 7 | **Org settings** | `global_settings` (singleton) | resource | Singleton shape; low ceremony, high value. |
| 8 | **Per-asset enrichment** | *(deny-list)* | none | Explicitly out of scope — documented below. |
| 9 | **Action workflows** | *(MEDIUM, experimental)* | resource | Cloud-only governance workflows; revisit when stable. |
| 10 | **Lineage / versioning / ER** | *(LOW)* | data source only | Manual lineage overwritten by ingestion. |
| 11 | **AI / Compass / Documents / Subscriptions / Page modules** | *(IRRELEVANT)* | none | Runtime/per-user/experimental Cloud features. |
| 12 | **Infrastructure / ops** | *(IRRELEVANT)* | none | Kafka, ES, K8s, RestoreIndices, Iceberg REST catalog, etc. |

---

## Category 1: Ingest

Current coverage: `datahub_ingestion_source`, `datahub_secret`, `datahub_remote_executor_pool` (+ data sources, except no `datahub_secret` data source).

| Operation | Type | Relevance | Cloud-only | Notes |
|---|---|---|---|---|
| `createIngestionSource` / `updateIngestionSource` / `deleteIngestionSource` / `ingestionSource(urn)` | M/Q | covered | no | `datahub_ingestion_source`. Gap: provider uses OpenAPI for writes; GraphQL mutations exist — worth reviewing but not a new TF component. |
| `createSecret` / `updateSecret` / `deleteSecret` + OpenAPI Read | M | covered | no | `datahub_secret`. |
| `createRemoteExecutorPool` / `updateRemoteExecutorPool` / `getRemoteExecutorPool` + OpenAPI Delete | M/Q | covered | yes | `datahub_remote_executor_pool`; mutations classed `category: internal`. |
| `updateDefaultRemoteExecutorPool` + `defaultRemoteExecutorPool` | M/Q | **HIGH** | yes | Singleton resource `datahub_default_remote_executor_pool` — or fold into `datahub_remote_executor_pool` via `is_default = true` (check if already present). **Note:** schema mutation is `updateDefaultRemoteExecutorPool`; confirm provider uses this name and not `setDefaultRemoteExecutorPool`. |
| `upsertConnection` / `connection(urn)` / `deleteConnection` | M/Q | **HIGH** | verify OSS | **New:** `datahub_connection` resource. Reusable credential-bearing config (endpoint + credentials for "prod-snowflake" etc.). Lets teams centralize credentials and scope rotation instead of inlining into recipe blobs. |
| `getRemoteExecutor` (instance) | Q | LOW | yes | Read-only. Intended backing query for `datahub_ingestion_executor` data source (Vikunja #404841). |
| `listIngestionSources` / `listSecrets` / `listRemoteExecutorPools` | Q | LOW | varies | Eventually consistent — data-source-only, never Read/Import. |
| `getSecretValues` | Q | LOW | no | Decrypted secret readout — doesn't fit, `value` is WriteOnly in state. |
| `ingestionSourceForEntity` | Q | LOW | no | Reverse lookup — niche data source candidate. |
| `createIngestionExecutionRequest` / `cancelIngestionExecutionRequest` / `rollbackIngestion` / `createTestConnectionRequest` | M | IRRELEVANT | no | Runtime/operational. |
| `executionRequest(urn)` / `listExecutionRequests` / `getRateLimitInfo` / executor telemetry family | Q/M | IRRELEVANT | varies | Run telemetry. |

**`datahub_connection`:** the most compelling new ingest resource. Connections are platform-instance configs (credentials + endpoint info) referenced by ingestion sources. Confirmed present on this Cloud GMS; OSS verification still required. `connection(urn)` provides strongly-consistent Read. URN key: user-supplied `id`.

---

## Category 2: Governance Taxonomy

The single largest HIGH bucket. All entities are slow-moving, governance/engineering-team-owned, and not touched by ingestion.

| Operation | Type | Relevance | Cloud-only | Notes |
|---|---|---|---|---|
| `createDomain` / `deleteDomain` / `moveDomain` + `domain(urn)` | M/Q | **HIGH** | no | **New:** `datahub_domain` resource + data source. URN concern: UI creates UUID-based URNs; provider must require explicit deterministic `id`. Reparenting via `moveDomain` maps to a `parent_urn` attribute update. |
| `createDataProduct` / `updateDataProduct` / `deleteDataProduct` + `dataProduct(urn)` | M/Q | **HIGH** | no | **New:** `datahub_data_product` resource + data source. Composition includes output-port URN list — aspect-list ownership applies. UI also creates UUID URNs — require explicit `id`. |
| `createGlossaryNode` + `deleteGlossaryEntity` + `glossaryNode(urn)` | M/Q | **HIGH** | no | **New:** `datahub_glossary_node` resource + data source (term folders/categories). Properties set via OpenAPI aspect PATCH after create. |
| `createGlossaryTerm` + `deleteGlossaryEntity` + `glossaryTerm(urn)` + scoped `updateName` / `updateDescription` / `updateParentNode` | M/Q | **HIGH** | no | **New:** `datahub_glossary_term` resource + data source. Shared `updateName`/`updateDescription` mutations are footguns — provider must scope to glossary URN types only. URN key: confirm SDK convention (hierarchical path vs UUID?). |
| `createTag` / `updateTag` / `deleteTag` / `setTagColor` + `tag(urn)` | M/Q | **HIGH** | no | **New:** `datahub_tag` resource + data source — **definitions only**. Assignments (`addTag`/etc.) are deny-list. `setTagColor` is a separate mutation; fold into the `update` lifecycle so name + description + color reconcile in one apply. |
| `createOwnershipType` / `updateOwnershipType` / `deleteOwnershipType` + OpenAPI entity | M/Q | **HIGH** | no | **New:** `datahub_ownership_type` resource + data source. Custom roles like "Steward", "Producer", "Technical Owner". Common dependency — many other resources reference these by URN. |
| `addRelatedTerms` / `removeRelatedTerms` | M | MEDIUM | no | Possible `datahub_glossary_term_relationship` resource (typed: isA, hasA, contains, values, relatedTerm). Aspect-list ownership applies. |
| `createApplication` / `updateApplication` / `deleteApplication` + `application(urn)` | M/Q | MEDIUM | verify | Newer entity type; semantic overlap with `data_product` unclear. Confirmed present on Cloud. Defer until stable. |
| `createGlossaryTermVersion` / versioning queries | M/Q | LOW | yes | Cloud-only temporal feature; not declarative. |
| `setDomain` / `unsetDomain` / `batchSetDomain` / `batchSet*Application` / `batchSet*DataProduct` | M | IRRELEVANT | no | Per-asset enrichment — deny-list. |
| `getRootGlossaryNodes` / `getRootGlossaryTerms` / `listDomains` / `listDataProductAssets` | Q | LOW | no | OpenSearch-backed; data-source-only with documented lag. |

**Glossary implementation notes:**
- `glossaryNode` and `glossaryTerm` need property aspects (`glossaryNodeInfo`, `glossaryTermInfo`) set via OpenAPI aspect PATCH after create — pure GraphQL does not cover all fields. Plan for create + aspect-write steps with rollback on failure.
- The shared `updateName`/`updateDescription`/`updateParentNode` mutations also apply to datasets, dashboards, etc. Provider must reject URN types it does not own.
- URN convention for glossary terms: verify against DataHub Python SDK before implementation. If the SDK uses a hierarchical path-derived ID, the provider must match it or risk duplicate-entity problems.

---

## Category 3: Schema Metadata

| Operation | Type | Relevance | Cloud-only | Notes |
|---|---|---|---|---|
| `createStructuredProperty` / `updateStructuredProperty` / `deleteStructuredProperty` + `structuredProperty(urn)` | M/Q | **HIGH** | no | **New:** `datahub_structured_property` resource + data source. Defines a typed custom property: allowed values, applicable entity types, cardinality. Schema-definition object — perfect IaC fit. |
| `createForm` / `updateForm` / `deleteForm` + `form(urn)` | M/Q | **HIGH** | no | **New:** `datahub_form` resource + data source. Metadata-collection forms (governance/compliance prompts). |
| `createDynamicFormAssignment` + `formInfo`/`dynamicFormAssignment` aspects | M | MEDIUM | verify | Declarative rule-based form assignment ("attach this form to all datasets in domain X"). Model as a nested attribute on `datahub_form` rather than a separate resource — assignment rules are a property of the form. |
| `createBusinessAttribute` / `updateBusinessAttribute` / `deleteBusinessAttribute` + `businessAttribute(urn)` | M/Q | MEDIUM | verify | Newer than structured properties; overlapping concept. Confirmed present on Cloud. Defer until patterns settle. |
| `upsertStructuredProperties` / `removeStructuredProperties` | M | IRRELEVANT | no | Per-asset value assignment — deny-list. |
| `batchAssignForm` / `batchRemoveForm` / `verifyForm` / `submitFormPrompt` / async batch variants | M | IRRELEVANT | mostly Cloud | Runtime form-filling — deny-list. |
| `addBusinessAttribute` / `removeBusinessAttribute` | M | IRRELEVANT | no | Per-asset assignment — deny-list. |
| `formAnalytics` / `formAnalyticsConfig` / `getFormsForActor` / `sendFormNotificationRequest` | Q/M | IRRELEVANT | yes | Telemetry/notifications. |

---

## Category 4: RBAC / Access / Identity

| Operation | Type | Relevance | Cloud-only | Notes |
|---|---|---|---|---|
| `createPolicy` / `updatePolicy` / `deletePolicy` + OpenAPI policy entity | M | **HIGH** | no | **New:** `datahub_policy` resource + data source. Covers platform policies (UI feature gating) and metadata policies (per-resource privileges). Heavy aspect-list ownership: privileges, resources, actors. Biggest design effort in the RBAC space. |
| `createGroup` / `removeGroup` / `updateCorpGroupProperties` + `corpGroup(urn)` | M/Q | **HIGH** | no | **New:** `datahub_corp_group` resource + data source (native groups only — do not manage IdP-synced groups). |
| `addGroupMembers` / `removeGroupMembers` | M | **HIGH** | no | **New:** `datahub_corp_group_member` resource. HashiCorp idiom: separate resource per binding, not a list on the group. |
| `createServiceAccount` / `deleteServiceAccount` + `getServiceAccount` | M/Q | **HIGH** | yes | **New:** `datahub_service_account` resource. Write-once secret pattern — `createServiceAccount` returns credentials; provider captures into state as `sensitive`. One-shot: no re-read of credentials after creation. Mirror `datahub_secret` value handling. |
| `upsertOAuthAuthorizationServer` / `deleteOAuthAuthorizationServer` + `oauthAuthorizationServer(urn)` | M/Q | **HIGH** | yes | **New:** `datahub_oauth_authorization_server` resource. Config for external OAuth IdPs. Cleanest "config" object in the access space. |
| `updateServiceAccountDefaultView` | M | MEDIUM | yes | Single attribute on `datahub_service_account`. |
| `createAccessToken` / `revokeAccessToken` + `getAccessToken` | M/Q | MEDIUM | no | Write-once tokens with TTL; rotation = constant churn. Skip unless customers ask. |
| `corpUser(urn)` | Q | MEDIUM | no | **New (data source only):** `datahub_corp_user` — useful for resolving usernames to URNs for owner/policy inputs. |
| `service(urn)` / `listServices` | Q | MEDIUM | yes | Cloud-only registered services — data source candidate. |
| `acceptRole` / `batchAssignRole` | M | LOW | no | Role-to-user assignment; typically governance-managed. |
| `createInviteToken` / `createNativeUserResetToken` / `sendUserInvitations` / `revokeUserInvitation` | M/Q | IRRELEVANT | no | One-shot invite/reset workflows. |
| `removeUser` / `updateUserStatus` / user-settings family | M | LOW/IRRELEVANT | no | Per-user state and UI prefs. |
| `resetLinkedIdentities` / `updateLinkedIdentities` | M | LOW | yes | SCIM/SSO identity linking; niche. |
| `listPolicies` / `listGroups` / `listUsers` / `listServiceAccounts` | Q | LOW | varies | OpenSearch-backed; data-source-only. |
| SCIM REST endpoints | REST | LOW | no | Provisioning usually owned by IdP. |
| SCIM-Configuration / auth-service-controller | REST | IRRELEVANT | no | Auth bootstrap surfaces. |

**`datahub_policy` note:** `PolicyUpdateInput` covers platform policies (UI gating) and metadata policies (per-resource access control). Lists for privileges, resources, and actors each need full-state ownership (aspect-list ownership rule). This is the highest-leverage RBAC resource.

**`datahub_service_account` note:** credentials returned once at creation; must be captured into state as `sensitive` and never shown in plan output. Import is impossible post-creation without recreating.

---

## Category 5: Tests / Metadata Tests

| Operation | Type | Relevance | Cloud-only | Notes |
|---|---|---|---|---|
| `createTest` / `updateTest` / `deleteTest` + `test(urn)` | M/Q | **HIGH** | verify OSS | **New:** `datahub_metadata_test` resource + data source. Declarative metadata-quality rules, e.g. "every PROD dataset must have an owner". `test(urn)` confirmed on Cloud; OSS presence needs verification. |
| `validateTest` | Q | MEDIUM | verify | Useful for plan-time syntax validation of test definitions. |
| `runTests` / `runTestDefinition` | M | IRRELEVANT | varies | Operational. |

---

## Category 6: Observe (Assertions, Monitors, Data Contracts, Incidents)

| Operation | Type | Relevance | Cloud-only | Notes |
|---|---|---|---|---|
| `upsertDataContract` + read via `entity()` or OpenAPI | M | **HIGH** | yes | **New:** `datahub_data_contract` resource. Bundles freshness/schema/quality/SLA assertions on a dataset. **Concerns:** (1) no dedicated `dataContract(urn)` GraphQL query confirmed missing on live probe — Read must use OpenAPI v3; (2) no dedicated `deleteDataContract` mutation — investigate soft-delete via aspect removal. Implement after `datahub_assertion_assignment_rule`. |
| `createAssertionAssignmentRule` / `updateAssertionAssignmentRule` / `deleteAssertionAssignmentRule` + `assertionAssignmentRule(urn)` | M/Q | **HIGH** | yes | **New:** `datahub_assertion_assignment_rule` resource. Declarative rule-based assignment of assertions/monitors to entities matching a filter — much higher leverage than per-asset assertions. Ship this first. |
| `createSqlAssertion` / `createDatasetAssertion` / `createFieldAssertion` / `createFreshnessAssertion` / `createVolumeAssertion` / `updateDatasetAssertion` / `deleteAssertion` / `upsertCustomAssertion` | M | MEDIUM | mostly Cloud | Per-asset assertions. Tension: config is TF-friendly but lifecycle is often runtime/UI-driven. Defer until rules + contracts are in place. |
| `upsertDataset*AssertionMonitor` (5 variants) / `createAssertionMonitor` / `updateAssertionMonitorSettings` / `updateMonitorStatus` / `deleteMonitor` | M | MEDIUM | yes | Cloud-only monitor management. Defer. |
| `assertion(urn)` / `listAssertions` | Q | HIGH/LOW | mostly Cloud | `assertion(urn)` could back a data source. |
| Run/report/backfill family | M/Q | IRRELEVANT | yes | Runtime/operational. |
| `proposeDataContract` | M | LOW | yes | Governance flow. |
| Incident lifecycle | M | IRRELEVANT | yes | Operational. |

---

## Category 7: Org-level Settings (Singleton Resources)

Each setting is a singleton (URN `urn:li:globalSettings:0` etc.). HashiCorp idiom: many small resources, not one fat one. Singleton shape: hard-code URN, no `Create` (just `Update`), treat as drift-correction. See `aws_default_*` for precedent.

| Operation | Type | Relevance | Cloud-only | Notes |
|---|---|---|---|---|
| `updateGlobalSettings` / `globalSettings` | M/Q | **HIGH** | no | **New:** `datahub_global_settings` resource. |
| `updateAssetSettings` | M | **HIGH** | yes | **New:** `datahub_asset_settings` resource. |
| `updateDocPropagationSettings` / `docPropagationSettings` | M/Q | MEDIUM | no | Doc propagation feature toggle. |
| `updateContextGenerationSettings` / `contextGenerationSettings` | M/Q | MEDIUM | yes | Ingestion context config. |
| `updateApplicationsSettings` | M | MEDIUM | yes | Applications feature toggle. |
| `updateGlobalViewsSettings` / `globalViewsSettings` | M/Q | MEDIUM | no | Org default view setting. |
| `updateSampleDataSettings` / `updateHelpLink` / `updateOrganizationDisplayPreferences` | M/Q | LOW | varies | UI prefs. |

---

## Category 8: Per-asset Enrichment (DENY-LIST)

**Do not implement these as TF resources.** They enrich individual data assets (datasets, charts, dashboards, ML models) whose metadata is owned by ingestion or business users. Managing them in Terraform creates an apply-overwrites-UI loop that erodes catalog trust.

**Tag assignments:** `addTag`, `addTags`, `batchAddTags`, `removeTag`, `batchRemoveTags`
**Term assignments:** `addTerm`, `addTerms`, `batchAddTerms`, `removeTerm`, `batchRemoveTerms`
**Owner assignments:** `addOwner`, `addOwners`, `batchAddOwners`, `removeOwner`, `batchRemoveOwners`
**Domain assignments:** `setDomain`, `unsetDomain`, `batchSetDomain`
**Data product assignments:** `batchSetDataProduct`, `batchAddToDataProducts`, `batchRemoveFromDataProducts`
**Application assignments:** `batchSetApplication`, `batchUnsetApplication`
**Structured-property values on assets:** `upsertStructuredProperties`, `removeStructuredProperties`
**Form assignments on assets:** `batchAssignForm`, `batchRemoveForm`, `refreshFormAssignment`, and all `verifyForm`/`submitFormPrompt`/async variants
**Business-attribute assignments:** `addBusinessAttribute`, `removeBusinessAttribute`
**Asset descriptions/names:** `updateDescription`, `updateShortDescription`, `updateName` (when applied to data assets), `updateDisplayProperties`, `updateDeprecation`, `batchUpdateDeprecation`, `batchUpdateSoftDeleted`, `updateEmbed`
**Per-type asset edits:** `updateDataset`, `updateDatasets`, `updateChart`, `updateDashboard`, `updateDataFlow`, `updateDataJob`, `updateNotebook`
**Institutional-memory links:** `addLink`, `removeLink`, `updateLink`, `upsertLink`
**Manual lineage edits:** `updateLineage`
**Generic passthrough:** `patchEntity`, `patchEntities`, `deleteReferences`, `reportOperation`

**Caveat:** setting `ownership` or `domains` *on a config entity that the provider manages* (e.g. the domain itself, a glossary term, a data product) is legitimate — the deny-list applies to these aspects on *data assets* (datasets, charts, etc.).

**Runtime/operational deny-list (also out of scope):** `runAssertion(s)`, `runAssertionsForAsset`, `testAssertion`, `reportAssertionResult`, `runTests`, `runTestDefinition`, `createIngestionExecutionRequest`, `cancelIngestionExecutionRequest`, `rollbackIngestion`, action-pipeline lifecycle mutations, incident lifecycle mutations, `bulkUpdateAnomalies`, `reportAnomalyFeedback`, `retryMonitorBackfill`, proposal accept/reject/propose mutations, all `*Notification*` mutations.

---

## Category 9: Action Workflows / Pipelines / Proposals (EXPERIMENTAL Cloud-only)

| Operation | Type | Relevance | Cloud-only | Notes |
|---|---|---|---|---|
| `upsertActionWorkflow` / `deleteActionWorkflow` / `listActionWorkflows` | M/Q | MEDIUM | yes | Governance/approval workflows. Configuration-flavored but early. Confirmed present on this Cloud instance. Revisit when stable. |
| `createActionPipeline` / `upsertActionPipeline` / `deleteActionPipeline` / `bootstrapActionPipeline` | M | MEDIUM | yes | Newer than action workflows; relationship unclear. Defer. |
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

## Category 11: AI / Compass / Documents / Subscriptions / Page Customization (IRRELEVANT)

All Cloud-only, runtime/per-user, or experimental. No TF resource shape:

- `dataHubAiConversation` family, AI memory, `inferDocumentation` — AI assistant
- `dataHubFile` family, `getPresignedUploadUrl` — file uploads
- `document` family, `searchDocuments` — wiki-style docs
- `subscription` family — per-user notifications
- `createAgent`/`updateAgent`/`deleteAgent`/`createTask`/`runTask` — AI agents (experimental)
- `createEval`/`runEvals` — AI evals (experimental)
- `upsertAiPlugin`/`deleteAiPlugin` — AI plugin config (borderline; revisit if customers ask)
- `upsertPageModule`/`deletePageModule`/`upsertPageTemplate`/`deletePageTemplate` — UI page customization
- `createDraftEntity` — governance drafts

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

---

## Cross-cutting implementation notes

### Stability classification

GraphQL has no inline `category: internal` markers. Stability is inferred from schema file location:

- **OSS-stable:** file present in `repos/datahub/datahub-graphql-core/src/main/resources/`
- **Cloud-stable:** `*.saas.graphql` / `*.acryl.graphql` in `repos/datahub-fork`
- **Cloud-experimental:** file only in fork, no OSS equivalent — includes `aiPlugin`, `agents`, `aimemory`, `evals`, `share`, `draft`, `actions`, `actions_pipeline`, `ai`, `constraints`, `versioning.glossary`

Resources in the experimental bucket carry no external stability guarantee. Apply the same disclaimer convention as `datahub_remote_executor_pool`.

### URN determinism (per design doc requirement)

Before implementing any new resource, confirm the URN key strategy and that it matches the DataHub Python SDK convention:

| Resource | URN key source | Concern |
|---|---|---|
| `datahub_connection` | user-supplied `id` | verify SDK convention |
| `datahub_policy` | user-supplied `id` | UI creates UUID; provider must require explicit `id` |
| `datahub_corp_group` | user-supplied `id` | confirm SDK convention |
| `datahub_data_product` | user-supplied `id` | UI creates UUID; provider must require explicit `id` |
| `datahub_domain` | user-supplied `id` | UI creates UUID; provider must require explicit `id` |
| `datahub_glossary_node` / `datahub_glossary_term` | hierarchical path or UUID? | confirm SDK convention before implementing |
| `datahub_tag` | user-supplied `name` | confirm normalization |
| `datahub_ownership_type` | user-supplied `id` | confirm |
| `datahub_structured_property` | user-supplied `id` | confirm |
| `datahub_form` | user-supplied `id` | confirm |
| `datahub_metadata_test` | user-supplied `id` | confirm |
| `datahub_data_contract` | derived from dataset URN | one contract per dataset |
| `datahub_assertion_assignment_rule` | user-supplied `id` | confirm |
| `datahub_service_account` | user-supplied `id` | Cloud-only; confirm |
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
| 1 | `datahub_connection` | resource | verify | URN convention; OSS vs Cloud |
| 2 | `datahub_domain` | resource + data source | yes | UUID URN trap; `moveDomain` for reparenting |
| 3 | `datahub_tag` | resource + data source | yes | Definitions only; `setTagColor` separate mutation |
| 4 | `datahub_glossary_node` | resource + data source | yes | OpenAPI aspect write after create; URN convention |
| 5 | `datahub_glossary_term` | resource + data source | yes | Shared mutations footgun; URN convention |
| 6 | `datahub_structured_property` | resource + data source | yes | Straightforward; pairs with `datahub_form` |

### Tier 2 — high leverage, larger design effort

| # | Terraform component | Type | OSS | Key concern |
|---|---|---|---|---|
| 7 | `datahub_data_product` | resource + data source | yes | Output-port list; domain reference; UUID URN trap |
| 8 | `datahub_policy` | resource + data source | yes | Fat input type; aspect-list ownership for 3 lists |
| 9 | `datahub_form` | resource + data source | yes | Prompts list; `dynamicFormAssignment` as nested attr |
| 10 | `datahub_metadata_test` | resource + data source | verify | OSS presence needs confirmation |
| 11 | `datahub_ownership_type` | resource + data source | yes | Small surface; common dependency |

### Tier 3 — Cloud-only, high leverage, accept stability caveat

| # | Terraform component | Type | Cloud | Key concern |
|---|---|---|---|---|
| 12 | `datahub_assertion_assignment_rule` | resource | yes | Ship before `data_contract` |
| 13 | `datahub_data_contract` | resource | yes | No GraphQL read query; delete strategy unclear |
| 14 | `datahub_service_account` | resource | yes | Write-once credentials; no import after create |
| 15 | `datahub_oauth_authorization_server` | resource | yes | Stable config object |
| 16 | `datahub_corp_group` + `datahub_corp_group_member` | resource + data source | yes | Only native groups; not IdP-synced groups |

### Tier 4 — defer or revisit

- `datahub_business_attribute` — overlaps with `structured_property`; revisit once patterns settle
- `datahub_application` — overlaps with `data_product`; defer until entity stabilizes
- `datahub_action_workflow` / `datahub_action_pipeline` — experimental Cloud; revisit when stable
- `datahub_global_settings` / settings family — singleton resources; low-ceremony add-ons
- `datahub_view` (global views only) — niche
- `datahub_post` (announcements) — niche
- `datahub_corp_user` (data source only) — useful URN lookup for owners/policy actors

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

Produced 2026-05-26. Sources:

1. **OpenAPI v2 REST** — `api-docs.json` (382 paths, 111 tags). Note: the spec is mostly v2; the v3 namespace is very small.
2. **GraphQL** — live introspection is gated on DataHub Cloud instances; schema enumerated from the DataHub GraphQL source tree (OSS + Cloud). 286 mutations + 157 queries.
3. **Live Cloud probes** — confirmed present on user's Cloud instance: `assertion`, `application`, `businessAttribute`, `connection`, `structuredProperty`, `test`, `form`, `document`, `role`, `listOAuthAuthorizationServers`, `listServiceAccounts`, `listActionWorkflows`, `listAssertionAssignmentRules`. Confirmed missing as top-level GraphQL query field: `dataContract` (use `entity()` or OpenAPI v3 instead).
