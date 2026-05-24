# Changelog

All notable changes to this provider will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.0] - 2026-05-24

### Added

- `datahub_remote_executor_pool` resource (DataHub Cloud only): create, update,
  and delete Remote Executor Pools. Supports `pool_id`, `description`, and
  `is_default`. Create waits for the pool to reach `READY` state before
  completing. Includes guards against deleting the embedded pool and a warning
  when deleting the current default pool.
- `datahub_remote_executor_pool` data source (DataHub Cloud only): look up an
  existing pool by `pool_id`, including the auto-provisioned `default` pool.
  Returns the pool's URN, `is_default`, `is_embedded`, `state_status`, and
  `channel` attributes.
- `examples/executor-pool-basic`: runnable example that provisions a pool and
  routes an ingestion source to it; includes copy-pasteable Helm values output.

### Changed

- `examples/ingestion-source-csv-enricher`: updated comment on `remote_executor_id`
  to refer users to `datahub_remote_executor_pool` for custom-pool use cases.

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

[Unreleased]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/datahub-project/terraform-provider-datahub/releases/tag/v0.1.0
