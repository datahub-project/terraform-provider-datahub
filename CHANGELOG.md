# Changelog

All notable changes to this provider will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `custom_properties` on `datahub_glossary_node`, `datahub_glossary_term`, `datahub_corp_user`, and `datahub_service_account`: a key-value string map stored on each entity's info aspect (`glossaryNodeInfo`/`glossaryTermInfo`/`corpUserInfo`), matching the `custom_properties` already on `datahub_domain` and `datahub_data_product`. Glossary nodes and terms render these properties in the DataHub UI, so the attribute gives a first-class home for governance metadata that previously had to be forced into the description. Terraform owns the complete map (keys added elsewhere are removed on the next apply), and the shared validator rejects empty maps, empty keys, and null or empty-string values at plan time. For glossary, the GraphQL create mutations do not carry `customProperties`, so the map is written via the OpenAPI v3 entity endpoint, passing the required aspect fields (`name`/`definition`, and `termSource` for terms) through so they are not clobbered; for users and service accounts it rides along in the existing `corpUserInfo` OpenAPI upsert.
- `datahub_structured_property_assignment` resource: assigns a structured property's value(s) to a target entity. Each resource is one `(entity, property)` edge (`entity_urn`, `structured_property_urn`, `values`), so multiple assignments can target the same entity - one per property - without clobbering each other: writes go through the per-property MERGE `upsertStructuredProperties` mutation, deletes through `removeStructuredProperties`, and read-back through the strongly-consistent OpenAPI v3 entity endpoint. This is the DataHub-native way to attach visible, governed metadata to platform entities (notably domains, which surface structured properties rather than custom properties). Values are string-typed in config; for a `number` property give the number in minimal string form (e.g. `"30"`). The value union, cardinality, and allowed-values are validated server-side. Supported targets are the platform-governance entities `domain`, `glossaryNode`, `glossaryTerm`, and `dataProduct`; other types (ingested data assets) are rejected at plan time - both because per-asset enrichment is out of the provider's scope and because DataHub silently no-ops such writes. The property definition's `entity_types` applicability is also enforced client-side at apply, since DataHub does not enforce it server-side.

## [0.13.0] - 2026-07-06

### Added

- `datahub_service_account` resource: manages a DataHub service account (a non-human identity for CI/CD, ingestion, and automation). Requires DataHub Core >= 1.4.0 or DataHub Cloud, and the `Manage Users & Groups` privilege. A service account is a `corpUser` carrying a `SERVICE_ACCOUNT` subtype under a `service_` URN prefix; the resource takes a user-supplied `service_account_id` and writes the `corpUserKey`, `corpUserInfo`, and `subTypes` aspects via OpenAPI v3, yielding a deterministic `urn:li:corpuser:service_<service_account_id>`. It deliberately does not call the GraphQL `createServiceAccount` mutation, which mints a random UUID id incompatible with Terraform's declarative model (the same UUID-bypass used by `datahub_ownership_type` and `datahub_domain`). Read and import are subtype-guarded: the resource refuses to manage a `corpUser` that is not a service account. Access tokens are minted separately (Settings -> Access Tokens) and are not managed here.
- `datahub_service_account` data source: looks up a service account by `service_account_id`, returning its URN and profile. Fails if the id resolves to a `corpUser` that is not a service account.
- `datahub_service_accounts` data source: returns the URNs of all service accounts (via the `listServiceAccounts` GraphQL query), for feeding an `import {}` for-each block to bulk-import existing service accounts. Eventually consistent - use for enumeration, not authoritative reads.
- `datahub_assertion_assignment_rule` resource: create and manage a DataHub Cloud assertion assignment rule -- a declarative rule that auto-assigns freshness and/or volume monitors to every dataset matching a search filter. One rule replaces hand-authoring a per-dataset assertion across many datasets: as new datasets match the filter, they are monitored automatically. Targets are expressed with `or_filters` (a disjunction of AND-groups of `{field, values, condition, negated}` facet predicates, mirroring DataHub's search filter model); optional `freshness` and `volume` blocks enable each monitor category with a `source_type` and incident `on_success_actions`/`on_failure_actions`. The URN is deterministic: `rule_id` (a URN suffix, derived from `name` when omitted) produces `urn:li:assertionAssignmentRule:<rule_id>`, created via `createAssertionAssignmentRule` and read from the strongly-consistent OpenAPI v3 entity endpoint. Requires DataHub Cloud; returns a clear diagnostic on OSS DataHub.
- `datahub_assertion_assignment_rules` data source: return the URNs of all DataHub Cloud assertion assignment rules for bulk import via `for_each` into `import {}` blocks. Backed by the `listAssertionAssignmentRules` GraphQL query. Requires DataHub Cloud.

### Fixed

- `datahub_structured_property`: allowed values containing a `/` (or `~`) now write successfully. The create/update path previously used the GraphQL `createStructuredProperty`/`updateStructuredProperty` mutations, whose server-side JSON-Patch builder splices each allowed value into an unescaped RFC-6901 JSON Pointer path - so a value like `SITS/eVision` was parsed as nested path segments and the write failed with `Invalid format for aspect: structuredProperty ... /allowedValues/0/value :: field is required` (a DataHub server bug affecting both OSS and Cloud). The provider now writes the structured-property definition (and settings) aspect via the OpenAPI v3 entity endpoint, which has no patch step and stores such values correctly; this also aligns the write path with the resource's existing OpenAPI read path and with how `datahub_domain`, `datahub_tag`, and `datahub_data_product` are already written. The update path, previously sent as GraphQL deltas, now writes the full desired definition - safe because the plan modifiers force replacement (not update) on any list shrink or cardinality narrowing.

## [0.12.0] - 2026-07-02

### Added

- `datahub_domain` resource and data source: added `custom_properties`, a key-value string map stored on the domain's `domainProperties` aspect. Domains support arbitrary custom properties in DataHub, but the resource previously exposed only `name`, `description`, and `parent_domain`, forcing that metadata into the description. Terraform owns the complete map (keys added elsewhere are removed on the next apply), mirroring `datahub_data_product`. Because the GraphQL `createDomain`/`updateName`/`updateDescription` mutations do not carry `customProperties`, the map is written via the OpenAPI v3 entity endpoint, passing `name`/`description`/`parentDomain` through so the values those mutations own are preserved.
- `examples/runnable/financial-services`: expanded the FIBO example into an end-to-end financial-services governance scenario. A `make`-driven Python pipeline downloads ISO 20022 message schemas from [iso20022.org](https://www.iso20022.org) and emits roughly 800 message types as Kafka topics, PostgreSQL tables, and Looker views with three-tier lineage; an LLM tagging pass maps each dataset and its individual columns to FIBO domains and glossary terms (for example the `Dbtr` and `Cdtr` columns of a pacs.008 credit transfer to the Debtor and Creditor terms); and a generated `assertions.tf` applies schema, volume, field, SQL, and freshness assertions across 26 representative tables. A `DEMO.md` runbook documents the resulting search, lineage, tagging, and data-quality navigation paths, each verified against a live instance.

### Changed

- `datahub_data_product`: `custom_properties` now rejects an empty map, empty keys, null values, and empty-string values at plan time. Previously these were silently coerced (a null value became `""`) or produced perpetual drift (an empty map read back as `null`). This is a behaviour change: a configuration that set any of those will now fail at plan with an actionable error - fix it by populating or removing the offending key, or omitting the attribute entirely. The same rule now applies to the new `datahub_domain.custom_properties`; both share one validator.
- Renamed the `examples/runnable/domain-hierarchy-fibo` example to `examples/runnable/financial-services`. The example outgrew its original domain-hierarchy scope - it now spans payments, securities, FX, collateral, and trade-finance message types with glossary, column-level tagging, lineage, and data-quality layers - so it is named for its industry vertical rather than its initial FIBO-domains contents. (Anyone referencing the old path should update it; the released 0.9.0 entry below keeps the old name as historical record.)
- `examples/runnable/financial-services`: the generated `assertions_config.json` is no longer committed. It is a ~7,000-line artifact rebuilt from the ISO 20022 cache by `make iso-assertions-config` into the gitignored `.iso-cache/` directory, and `assertions.tf` reads it through a `fileexists` guard that plans zero assertions when the file is absent (matching the FIBO cache behaviour in `main.tf`).
- README: documented the typed assertion resources (`datahub_schema_assertion`, `datahub_volume_assertion`, `datahub_field_assertion`, `datahub_sql_assertion`, `datahub_freshness_assertion`, `datahub_custom_assertion`), `datahub_action_pipeline`, `datahub_data_product`, and `datahub_ownership_type`, and their data sources, in the "What it supports" tables - these shipped in earlier releases but were missing from the README.

## [0.11.0] - 2026-06-29

### Added

- `datahub_action_pipeline` resource: create and manage a DataHub Cloud action pipeline (automation) -- a packaged action that runs a recipe to propagate metadata (descriptions, tags, glossary terms) back to a platform such as BigQuery or Dataplex. The resource manages the pipeline definition (`name`, `type`, `recipe`, `category`, `description`, `executor_id`, `version`, `debug_mode`); the `recipe` is a JSON string compared by semantic equality (like `datahub_ingestion_source`), and `${SECRET_NAME}` placeholders are stored verbatim and resolved at execution time. The URN is deterministic: `action_id` (a URN suffix, derived from `name` when omitted) produces `urn:li:dataHubAction:<action_id>`, written via `upsertActionPipeline`. Requires DataHub Cloud; returns a clear diagnostic on OSS DataHub.
- `datahub_action_pipelines` data source: return the URNs of all DataHub Cloud action pipelines for bulk import via `for_each` into `import {}` blocks. Backed by the `listActionPipelines` GraphQL query. Requires DataHub Cloud.

### Changed

- `datahub-tf-extract`: fails fast when the `--output` directory already contains a `generated.tf` from a previous run -- a re-run previously left stale, partial config in place while still reporting success, because `terraform plan -generate-config-out` refuses to overwrite the file. A genuine validation-plan failure is now fatal rather than a swallowed warning.
- Import guide (`docs/guides/import-existing.md`): corrected the write-only resource note (the first post-import apply plans a one-time replacement, not a no-op, because `*_wo_version` imports as null), noted that shared-instance narrowing bypasses the tool's post-processing, and added a "Migrating from another Terraform provider" section for provider-swap migrations.

## [0.10.0] - 2026-06-18

### Added

- `datahub_field_assertion` resource: create and manage a DataHub field (column) assertion monitor. Two sub-types: `FIELD_VALUES` checks every row's value against an `operator` and value (with optional `transform_type` such as `LENGTH`, a `fail_threshold_type`/`fail_threshold_value` tolerance, and `exclude_nulls`), and `FIELD_METRIC` checks an aggregate column metric (one of 17 kinds: `NULL_COUNT`, `UNIQUE_COUNT`, `MIN`, `MAX`, `MEAN`, `STDDEV`, etc.). The column is described by `field_path`/`field_type`/`field_native_type`. A plan-time validator enforces the sub-type split (`metric` required for `FIELD_METRIC`, rejected for `FIELD_VALUES`). `FIELD_VALUES` requires a warehouse-backed platform (BigQuery, Snowflake, Redshift, Databricks) and a query `source_type`; `FIELD_METRIC` can evaluate against a previously ingested dataset profile. Requires DataHub Cloud; returns a clear diagnostic on OSS DataHub. URN is server-generated.
- `datahub_schema_assertion` resource: create and manage a DataHub schema assertion monitor. Asserts that a dataset's columns match an expected set, catching unexpected schema drift, with a `compatibility` mode (`EXACT_MATCH`, `SUPERSET`, `SUBSET`). The resource owns the complete expected `fields` list (each `path`/`type`/`native_type`). On read DataHub returns each field's standard type as a `SchemaFieldDataType` class object rather than the plain string sent on write; the provider maps the class back to the standard type so the resource stays drift-free. Requires DataHub Cloud; returns a clear diagnostic on OSS DataHub. URN is server-generated.
- `datahub_volume_assertion`: `ROW_COUNT_CHANGE` (growth) sub-type, selected via `volume_type = "ROW_COUNT_CHANGE"` plus a new `change_type` attribute (`ABSOLUTE` or `PERCENTAGE`), alongside the existing `ROW_COUNT_TOTAL`. The operator and value attributes are reused, so all NATIVE volume assertions are now expressible in one resource.
- `datahub_sql_assertion`: `METRIC_CHANGE` sub-type, selected via `sql_type = "METRIC_CHANGE"` plus a `change_type` attribute (`ABSOLUTE` or `PERCENTAGE`). `METRIC_CHANGE` requires a non-empty `description` (DataHub rejects the mutation otherwise), enforced at plan time.
- `datahub_freshness_assertion`: `SINCE_THE_LAST_CHECK` schedule type, which asserts the dataset changed at all between consecutive evaluations and takes no window sub-configuration, alongside `FIXED_INTERVAL` and `CRON`. A new config validator ties the window sub-fields to the chosen `schedule_type`.
- Cross-cutting assertion inputs: `description` on `datahub_volume_assertion` and `datahub_freshness_assertion` (`datahub_sql_assertion` already had it); `filter_sql` (a row-level SQL filter) on `datahub_volume_assertion` and `datahub_freshness_assertion`; and `failure_severity` (`LOW`/`MEDIUM`/`HIGH`) on the freshness, sql, and field assertions.
- `datahub-tf-extract` and the `datahub_assertions` data source now enumerate the new `datahub_field_assertion` and `datahub_schema_assertion` resources for bulk import.

### Changed

- `datahub-tf-extract`: hardened enumeration and unified the import-target registry so the CLI enumerates every importable resource type (previously only a subset), with system-source filtering and URN de-duplication.
- Assertion enumeration and import are now scoped to NATIVE (author-as-code) assertions. Ingested `EXTERNAL` assertions (e.g. dbt tests, Great Expectations) and smart/AI `INFERRED` assertions are never enumerated, and a direct `terraform import` of a non-NATIVE assertion into a typed resource is refused with a clear diagnostic, since those are owned and regenerated by the producing system.
- `datahub_ingestion_source`: the `recipe` attribute now uses JSON semantic equality (`jsontypes.Normalized`), so key ordering and whitespace differences no longer produce spurious plan diffs. Existing state is normalized once on the next apply.
- Go toolchain updated to 1.26.4; pinned `mise` tool versions refreshed.

### Fixed

- ImportState of `datahub_freshness_assertion`, `datahub_volume_assertion`, and `datahub_sql_assertion` now recovers the monitor-side fields (evaluation schedule, source type, and mode) from the associated Monitor entity. Previously these were not read back, so an imported assertion produced a spurious diff on the first plan; imports now re-plan cleanly.

## [0.9.0] - 2026-06-11

### Added

- `datahub_custom_assertion` resource: create and manage custom (external) DataHub assertion definitions. Custom assertions are evaluated by an external system (dbt tests, Great Expectations, custom scripts) and reported back to DataHub via `reportAssertionResult`. The resource declares the assertion and associates it with a dataset URN; it does not execute the assertion itself. Works on both OSS DataHub and DataHub Cloud. DataHub generates a server-side UUID for the URN on first creation; the provider stores and reuses it on all subsequent updates, passing the existing URN back to the `upsertCustomAssertion` mutation to guarantee idempotent upserts.
- `datahub_freshness_assertion` resource: create and manage a DataHub freshness assertion monitor. Freshness assertions check that a dataset has been updated within an expected window, evaluated on a configurable cron schedule. Supports `FIXED_INTERVAL` (rolling window, e.g. data must arrive every 24 hours) and `CRON` (calendar window) schedule types. Requires DataHub Cloud; returns a clear diagnostic on OSS DataHub. URN is server-generated.
- `datahub_volume_assertion` resource: create and manage a DataHub volume assertion monitor. Volume assertions check that a dataset has an expected row count at evaluation time. Supports `DATAHUB_DATASET_PROFILE` source type (evaluates against a previously ingested DatasetProfile - no live database query required), `INFORMATION_SCHEMA`, and `QUERY`. Requires DataHub Cloud; returns a clear diagnostic on OSS DataHub. URN is server-generated.
- `datahub_sql_assertion` resource: create and manage a DataHub SQL assertion monitor. SQL assertions run a custom SELECT statement and compare the numeric result to an expected value, enabling business-logic checks (no negative values, referential integrity counts, etc.) that volume and freshness assertions cannot express. Requires DataHub Cloud; returns a clear diagnostic on OSS DataHub. URN is server-generated.
- `datahub_assertion` data source: look up an existing DataHub assertion by URN and return its type and target entity URN. Use this to reference an assertion created outside Terraform without taking ownership of it.
- `datahub_assertions` data source: return the URNs of all DataHub assertions for bulk import via `for_each` into `import {}` blocks. Backed by `searchAcrossEntities` (OpenSearch).
- `examples/runnable/assertion-volume-sqlite`: runnable example demonstrating a volume assertion evaluated against a locally-seeded SQLite dataset profiled via the DataHub CLI. Includes a Python seed script and a README walkthrough of the PASS-FAIL-PASS cycle using `DATAHUB_DATASET_PROFILE` source type (no live database query from DataHub Cloud).

### Fixed

- `datahub_custom_assertion` destroy on OSS DataHub: the `deleteAssertion` GraphQL mutation rejects CUSTOM type with "Unsupported Assertion Type CUSTOM". The provider now falls back to the OpenAPI v3 entity endpoint for CUSTOM type deletes, which works on both OSS and Cloud.
- `datahub_custom_assertion` resource and `datahub_assertion` data source: `entity_urn` was empty when reading assertions from OSS DataHub. OSS assertionInfo schema v3 stores the entity URN inside `customAssertion.entity` rather than the top-level `entityUrn` field added in Cloud schema v4. Both read paths now fall back to `customAssertion.entity` when `entityUrn` is absent.
- `datahub_freshness_assertion`, `datahub_volume_assertion`, `datahub_sql_assertion`: an immediate update after create on DataHub Cloud could fail with "Monitor for assertion X does not exist." DataHub Cloud creates the linked monitor entity asynchronously after the upsert mutation returns; the provider now polls until the monitor is visible before returning from Create.
- `examples/runnable/domain-hierarchy-fibo`: added a `terraform plan`-time precondition that fails with a clear message (`FIBO cache is missing or stale. Run: make fibo-update`) when the `.fibo-cache/fibo.json` file is absent or was generated by an older version of the build script. Previously, a missing cache produced a confusing Terraform type-consistency error with no actionable guidance.

## [0.8.0] - 2026-06-08

### Added

- `datahub_data_product` resource: create and manage DataHub data product definitions with a deterministic, user-supplied `data_product_id` (URN suffix). Manages the product definition -- `name`, `description`, optional `domain` (full domain URN), `external_url`, and `custom_properties` -- but not asset membership. Member datasets, charts, and other assets are intentionally out of scope: asset membership is per-asset enrichment and is managed via the DataHub UI, CLI, or SDK without interference from `terraform apply`. Create and update write the `dataProductProperties` and `domains` aspects directly via the OpenAPI v3 endpoint, not the GraphQL mutations, because `createDataProduct`/`updateDataProduct` cannot set `external_url` or `custom_properties`. The DataHub UI creates data products with a random UUID when no id is supplied; the provider requires an explicit `data_product_id` to produce stable, importable URNs that match the DataHub Python SDK convention (`make_data_product_urn(id)`).
- `datahub_data_product` data source: look up an existing data product by `data_product_id` and return its URN, name, description, domain, external URL, and custom properties. Use this to reference a data product created outside Terraform without taking ownership of it.
- `datahub_data_products` data source: return the URNs of all DataHub data products for bulk import via `for_each` into `import {}` blocks. Backed by the `searchAcrossEntities` GraphQL query (entity type `DATA_PRODUCT`).
- `examples/runnable/data-product-simple`: runnable example creating a domain and two data products (Orders, Customer 360), demonstrating the resource with custom properties, the singular data source, and the plural list data source.
- `datahub_ownership_type` resource: create and manage custom DataHub ownership type definitions with a deterministic, user-supplied `type_id` (URN suffix). Ownership types are named roles assigned to asset owners (e.g. "Data Quality Lead", "Data Producer") visible throughout the DataHub UI. Create and update write the `ownershipTypeInfo` aspect directly via the OpenAPI v3 endpoint - the GraphQL `createOwnershipType` mutation is not used because it generates a server-side random UUID for the id, making URNs non-deterministic and unmanageable by Terraform. This matches the DataHub Python SDK convention (`make_ownership_type_urn(id)`). The four built-in system types (`__system__technical_owner`, `__system__business_owner`, `__system__data_steward`, `__system__none`) cannot be managed or deleted; `type_id` values beginning with `__system__` are rejected at plan time.
- `datahub_ownership_type` data source: look up an existing ownership type by `type_id` and return its URN, name, and description. Works for both custom types and built-in system types. Use this to reference a built-in type's URN (e.g. `__system__technical_owner`) without taking ownership of it.
- `datahub_ownership_types` data source: return the URNs of all DataHub ownership types (custom and system) for bulk import via `for_each` into `import {}` blocks. Backed by the `listOwnershipTypes` GraphQL query.
- `examples/runnable/ownership-type-simple`: runnable example creating two custom ownership types (Data Quality Lead, Data Producer), reading back a custom type and the built-in Technical Owner type via the singular data source, and enumerating all ownership types via the plural data source.

## [0.7.0] - 2026-06-07

### Added

- `datahub_structured_property` resource: create and manage DataHub structured property definitions with a deterministic, user-supplied `property_id` (URN suffix and `qualifiedName`). Manages the property schema -- `value_type` (`string`, `number`, `date`, `urn`, `rich_text`), `cardinality` (`SINGLE`/`MULTIPLE`), `entity_types` (which asset types the property can be applied to), optional `allowed_values` enum constraint, optional `allowed_entity_types` filter for `urn`-typed properties, and the `structuredPropertySettings` display-flag aspect via a `settings {}` block. This resource manages the definition only; applying values to individual assets is per-asset enrichment and is out of scope. The DataHub `updateStructuredProperty` mutation is append-only for list fields (`entity_types`, `allowed_values`, `allowed_entity_types`) and cardinality can only widen `SINGLE`->`MULTIPLE`: additive changes are applied in-place, while removing an element or narrowing cardinality forces resource replacement (which hard-deletes the property and removes all applied values from assets).
- `datahub_structured_property` data source: look up an existing structured property definition by `property_id` and return all fields. Use this to reference a property created outside Terraform without taking ownership of it.
- `datahub_structured_properties` data source: return the URNs of all DataHub structured properties for bulk import via `for_each` into `import {}` blocks.
- `examples/runnable/structured-property-simple`: runnable example creating a `number`-typed retention-days property (dataset) and a `string`-typed classification property with allowed values (dataset + dashboard), demonstrating the resource, singular data source, and plural list data source.
- `datahub_tag` resource: create and manage DataHub tags with a deterministic, user-supplied `tag_id` (URN suffix). Manages the tag entity itself -- its display `name`, `description`, and optional `color_hex` display colour (`#RRGGBB` format) -- not where the tag is applied to data assets. Tags are flat (no parent/child hierarchy). Create uses the `createTag` GraphQL mutation; colour is set via the dedicated `setTagColor` mutation; renames write the `tagProperties` aspect directly via OpenAPI v3 (the DataHub `updateName` mutation does not support the Tag entity type).
- `datahub_tag` data source: look up an existing tag by `tag_id` and return its URN, name, description, and colour. Use this to reference a tag created outside Terraform without taking ownership of it.
- `datahub_tags` data source: return the URNs of all DataHub tags for bulk import via `for_each` into `import {}` blocks.
- `examples/runnable/tag-simple`: runnable example creating three tags (PII, Verified, Deprecated) with distinct colours, demonstrating the resource, singular data source, and plural list data source.

## [0.6.0] - 2026-06-06

### Added

- `datahub_glossary_node` resource: create and manage DataHub glossary nodes (the "Term Groups" shown in the DataHub UI) with a deterministic, user-supplied `node_id` (URN suffix). Nodes can be nested to any depth via an optional `parent_node` attribute. Set `parent_node` to another `datahub_glossary_node` resource's `.urn` attribute so Terraform's dependency graph creates parents before children and destroys children before parents. Unlike domains, DataHub does not refuse to delete a node that still has children, so correct ordering via `.urn` references is the only ordering guarantee. Reparenting is performed in place via `updateParentNode` without forcing replacement.
- `datahub_glossary_node` data source: look up an existing term group by `node_id` and return its URN, name, description, and parent node. Use this to reference an unmanaged node as a `parent_node` input without taking ownership of it.
- `datahub_glossary_nodes` data source: return the URNs of all DataHub glossary nodes for bulk import via `for_each` into `import {}` blocks.
- `datahub_glossary_term` resource: create and manage DataHub glossary terms (the "Terms" shown in the DataHub UI) with a deterministic, user-supplied `term_id` (URN suffix, max 56 characters). Terms live under a `datahub_glossary_node` via the `parent_node` attribute; terms cannot be parents of other terms. Reparenting (including detaching to root) is performed in place via `updateParentNode` without forcing replacement.
- `datahub_glossary_term` data source: look up an existing term by `term_id` and return its URN, name, description, and parent node.
- `datahub_glossary_terms` data source: return the URNs of all DataHub glossary terms for bulk import via `for_each` into `import {}` blocks.
- `domain` attribute on `datahub_glossary_node` and `datahub_glossary_term`: associate a glossary entity with a DataHub domain by setting this attribute to a domain URN (e.g. `datahub_domain.finance.urn`). The association is managed via the `setDomain`/`unsetDomain` GraphQL mutations and is read back from the `domains` aspect on the strongly-consistent OpenAPI v3 endpoint.

### Changed

- Extracted the `updateName` and `updateDescription` GraphQL mutation wrappers into shared client helpers (`UpdateEntityName`, `UpdateEntityDescription`) reused by domains, glossary nodes, and glossary terms.
- Extracted `SetEntityDomain` and `UnsetEntityDomain` as shared client helpers wrapping the `setDomain`/`unsetDomain` GraphQL mutations, available for reuse by future resources that support domain association.

## [0.5.0] - 2026-06-05

### Added

- `datahub_domain` resource: create and manage DataHub domains with a deterministic, user-supplied `domain_id` (URN suffix). Domains can be nested to any depth via an optional `parent_domain` attribute. Set `parent_domain` to another `datahub_domain` resource's `.urn` attribute so Terraform's dependency graph creates parents before children and destroys children before parents — DataHub hard-deletes domains and refuses deletion if any child domains exist. Reparenting is performed in place via `moveDomain` without forcing replacement.
- `datahub_domain` data source: look up an existing domain by `domain_id` and return its URN, name, description, and parent domain. Use this to reference an unmanaged domain (created outside Terraform) as a `parent_domain` input without taking ownership of it.
- `datahub_domains` data source: return the URNs of all DataHub domains across the full hierarchy, for bulk import via `for_each` into `import {}` blocks.

### Fixed

- Import guide template was not updated when the extract tool archive was renamed to `tools-datahub-tf-extract` in v0.4.1, causing `make generate` to revert the already-corrected `docs/guides/import-existing.md` on every run.

## [0.4.1] - 2026-06-04

### Fixed

- Release packaging: the `datahub-tf-extract` zip archives were included in the provider `SHA256SUMS` file and sorted alphabetically before `terraform-provider-datahub`, causing the Terraform Registry to serve the extract tool zip instead of the provider zip. `terraform init` failed with "provider binary not found" on all platforms. Fixed by renaming the extract tool archives to `tools-datahub-tf-extract_*` (which sorts after `terraform-provider-datahub_*`). The binary name inside the zip (`datahub-tf-extract`) is unchanged. Update any `mise.toml` `ubi` entries to use `matching = "tools-datahub-tf-extract"`.

## [0.4.0] - 2026-06-01

### Added

- `datahub_corp_group` resource: create and manage native DataHub groups with a deterministic, user-supplied `group_id` (URN suffix). Manages display name, description, email, and Slack handle. Membership is managed separately via `datahub_corp_group_member` so users and bindings compose independently.
- `datahub_corp_group_member` resource: manage a single membership edge (one user in one native group) as its own resource, following the HashiCorp idiom. Membership is stored on the user's `nativeGroupMembership` aspect; existence is read via the strongly-consistent OpenAPI v3 path. Import by composite ID (`<group_urn>|<user_urn>`).
- `datahub_role_assignment` resource: assign a built-in DataHub role (`Admin`, `Editor`, `Reader`) to a user or group. DataHub enforces one role per actor, so the actor URN is the resource key and reassignment is in place; deleting clears the role. After create the assignment is read back to surface an error if the actor does not exist (the API silently skips unknown actors).
- `datahub_role` data source: resolve a built-in role name to its URN, description, and editability.
- `datahub_roles` data source: return the URNs of all built-in roles.
- `datahub_policy` resource: create and manage DataHub access policies (PLATFORM and METADATA) with a deterministic, user-supplied `policy_id`. Grants a set of privileges to a set of actors (users/groups, or all-users/all-groups/resource-owners), optionally scoped to resources. Privileges and actors are modeled as sets (order-insensitive) and the resource owns the full state, writing it on every apply. Created and updated via `updatePolicy` at the deterministic URN (avoiding the UI's random UUID); read via the strongly-consistent OpenAPI v3 path.
- `datahub_policies` data source: return the URNs of all policies (including DataHub's default system policies), for bulk import.
- `datahub_corp_group` data source: look up an existing group by `group_id` and return its URN and properties, for use as a policy actor or owner reference.
- `datahub_corp_groups` data source: return the URNs of all groups, for bulk import via `for_each` into `import {}` blocks.
- `datahub_corp_user` resource: create and manage a DataHub user's catalog profile (`corpUserInfo` aspects) with upsert semantics. Works for new users and pre-existing ones created by SSO/JIT provisioning, metadata ingestion, or `datahub_local_user_login`. Delete hard-deletes the user entity.
- `datahub_corp_user` data source: resolve a `username` to its URN and catalog metadata (display name, email, title, active, status).
- `datahub_local_user_login` resource: provision native-auth login credentials for a DataHub user via the signUp flow. When `initial_password` is omitted, generates a random throwaway password and exposes a single-use 24h reset URL (`password_reset_url`) so the user sets their own password -- Terraform never holds a real credential. Works on both OSS DataHub and DataHub Cloud (on Cloud, `username` must be the user's email address). Delete hard-deletes the entire user entity. Requires Terraform CLI 1.11+.
- `frontend_url` optional provider config: explicit DataHub frontend URL for native user operations. Derived automatically from `gms_url` when not set.
- `examples/runnable/local-iam`: runnable example demonstrating the full IAM stack -- a login user, a catalog-only service/pipeline account, group membership, a role assignment, and an access policy for a team.

## [0.3.0] - 2026-05-29

### Added

- `datahub-tf-extract` CLI: `enumerate` command extracts an existing brownfield DataHub deployment as Terraform configuration. Enumerates all resources of each registered type, writes `import {}` blocks, drives `terraform plan -generate-config-out`, and post-processes the output to insert `var.*` references for WriteOnly attributes (secrets) and platform-block stubs for connections. Run `terraform apply` on the output directory to perform the actual import into Terraform state. Eliminates the need to hand-author hundreds of resource blocks and hunt down URNs manually.
- `datahub_ingestion_source` resource: `terraform import` support. Import by full URN (`urn:li:dataHubIngestionSource:<id>`) or bare `source_id`. All non-credential fields are populated from the server on import.
- `datahub_connection` resource: create, update, and delete DataHub Connections -- reusable, encrypted credential configurations for data platforms (Databricks, Snowflake, BigQuery, Redshift, Unity Catalog) and any other platform via a generic `raw_config` escape hatch. Connection credentials are never stored in Terraform state (WriteOnly). Drift detection covers `name` and `platform` via the strongly-consistent OpenAPI v3 read path. Credential rotation is triggered by incrementing `config_wo_version`. Requires Terraform CLI 1.11+.
- `datahub_ingestion_sources` data source: returns the URNs of all ingestion sources visible to the authenticated principal. Useful as a `for_each` input to `import {}` blocks when bulk-importing a brownfield deployment.
- `datahub_secrets` data source: returns the URNs of all secrets. Secret values are never returned -- only URNs are exposed.
- `datahub_connections` data source: returns the URNs of all connections. Backed by `searchAcrossEntities` with entity type `DATAHUB_CONNECTION`.
- Import-target registry (`internal/provider/importtarget`): every resource now registers an enumeration function and import-ID extractor. A CI test (`TestImportTargetCoverage`) enforces that all resources either have a registry entry or an explicit exemption, preventing new resources from being silently excluded from the bulk-import workflow.

### Fixed

- `datahub_connection` on OSS DataHub: `deleteConnection` GraphQL mutation does not exist in OSS. Delete now uses `DELETE /openapi/v3/entity/datahubconnection/{urn}`, which is safe because connection deletion does not require the encryption service layer.
- `datahub_connection` on OSS DataHub: the entity endpoint omits `platform` from the response. `Read` previously overwrote state with the empty string, causing a "produced inconsistent result after apply" error on the next plan. Platform is now only updated when the API returns a non-empty value.
- `datahub_connection` `ImportState` on OSS DataHub: `nullBlockForPlatform("")` incorrectly populated `raw_config` with two null fields when the platform was unknown, causing `ImportStateVerify` failures. All platform blocks are now left nil when the platform cannot be determined from the API response.

## [0.2.0] - 2026-05-25

### Added

- `datahub_ingestion_source` data source: look up an existing ingestion source by
  `source_id`. Returns all resource attributes (`source_name`, `source_type`, `recipe`,
  schedule, executor, etc.) as read-only outputs.
- `datahub_remote_executor_pool` resource (DataHub Cloud only): create, update,
  and delete Remote Executor Pools. Supports `pool_id`, `description`, and
  `is_default`. Create waits for the pool to reach `READY` state before
  completing. Includes guards against deleting the embedded pool and a warning
  when deleting the current default pool.
- `datahub_remote_executor_pool` data source (DataHub Cloud only): look up an
  existing pool by `pool_id`, including the auto-provisioned `default` pool.
  Returns the pool's URN, `is_default`, `is_embedded`, `state_status`, and
  `channel` attributes.
- `examples/runnable/executor-pool-basic`: runnable example that provisions a pool and
  routes an ingestion source to it; includes copy-pasteable Helm values output.
- Availability badges (`DataHub ✅ | DataHub Cloud ✅` or `DataHub ❌ | DataHub Cloud ✅`)
  on every resource and data source schema description so users can see at a glance
  which surfaces require DataHub Cloud.

### Changed

- `examples/runnable/ingestion-source-csv-enricher`: updated comment on `remote_executor_id`
  to refer users to `datahub_remote_executor_pool` for custom-pool use cases.
- Provider index page (`docs/index.md`): rewritten description focusing on what the
  provider manages and what it does not; page title now renders as "DataHub Provider"
  (was "datahub Provider"); example usage updated to env-var-first pattern with a
  `datahub_me` credential validator.

### Fixed

- Internal 404 handling: replaced string-matching on `"not found"` in HTTP error
  bodies with an `ErrNotFound` sentinel value throughout the client layer. All
  resources and data sources now handle not-found consistently via `errors.Is`.

**API stability notice.** The GraphQL mutations backing `datahub_remote_executor_pool`
are classified as `internal` in DataHub Cloud and carry no external API stability
guarantee. See the resource documentation for details.

## [0.1.0] - 2026-05-23

Initial public release.

### Added

- `datahub_ingestion_source` resource: manage DataHub ingestion sources
  including schedule, executor, recipe, and platform configuration.
- `datahub_secret` resource: manage DataHub secrets with server-side
  AES-GCM-256 encryption. The `value` attribute is WriteOnly and never
  stored in Terraform state. Requires Terraform CLI 1.11+.
- `datahub_me` data source: read the authenticated principal's URN,
  username, display name, and email.
- Provider authentication via `gms_url`/`gms_token` block attributes,
  `DATAHUB_GMS_URL`/`DATAHUB_GMS_TOKEN` environment variables, or
  `~/.datahubenv` (DataHub CLI config).

[Unreleased]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.13.0...HEAD
[0.13.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.12.0...v0.13.0
[0.12.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.11.0...v0.12.0
[0.11.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.10.0...v0.11.0
[0.10.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/datahub-project/terraform-provider-datahub/releases/tag/v0.1.0
