# Changelog

All notable changes to this provider will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
- `datahub_corp_user` data source: resolve a `username` to its URN and catalog metadata (display name, email, title, active, status). This provider does not create users; this data source reads users provisioned via SSO/JIT or the DataHub invite flow.
- `examples/runnable/authorization-team-access`: runnable example that sets up authorization for a team. Starts with a group and its lookup; grows across the IAM series to add membership, a role assignment, and an access policy.

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

[Unreleased]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/datahub-project/terraform-provider-datahub/releases/tag/v0.1.0
