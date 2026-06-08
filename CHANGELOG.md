# Changelog

All notable changes to this provider will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.8.0...HEAD
[0.8.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/datahub-project/terraform-provider-datahub/releases/tag/v0.1.0
