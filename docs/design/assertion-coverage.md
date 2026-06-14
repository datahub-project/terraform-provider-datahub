# Assertion resource coverage vs the DataHub API

What each `datahub_*_assertion` resource can express today, measured against the live DataHub Cloud GraphQL schema.

**Method:** live introspection (`__type`) of the `upsertDataset{Volume,Freshness,Sql}AssertionMonitorInput` input types and their enums against a DataHub Cloud instance, June 2026. Introspection is enabled, but a `BadFaithIntrospection` guard rejects queries containing many `__type` selections at once -- introspect one type per request.

Legend: yes = expressible; **no** = not expressible by the resource; passthrough = the resource accepts a free-form string so the value is usable even though only a subset is documented.

## datahub_volume_assertion

`UpsertDatasetVolumeAssertionMonitorInput` fields: `entityUrn, description, type, inferWithAI, inferenceSettings, rowCountTotal, rowCountChange, filter, actions, evaluationSchedule, evaluationParameters, mode, executorId, backfillConfig`.

| Capability | API | Resource |
|---|---|---|
| Sub-type `ROW_COUNT_TOTAL` ("row count is …") | yes | yes |
| Sub-type `ROW_COUNT_CHANGE` ("row count growing by …") | yes | **no** (client hardcodes `rowCountTotal`) |
| Change type `ABSOLUTE` / `PERCENTAGE` (for change) | yes | **no** |
| Operators (`AssertionStdOperator`, 18 values) | 18 | 6 (`BETWEEN, LESS_THAN, LESS_THAN_OR_EQUAL_TO, GREATER_THAN, GREATER_THAN_OR_EQUAL_TO, EQUAL_TO`) |
| Source type (`DatasetVolumeSourceType`): `INFORMATION_SCHEMA, QUERY, TABLE_STATISTICS, DATAHUB_DATASET_PROFILE, PLATFORM_API` | 5 | passthrough (3 documented) |
| `filter` (row-level filter) | yes | **no** |
| `inferWithAI` + `inferenceSettings` (smart/auto thresholds) | yes | **no** |
| `failureSeverityConfig` | yes | **no** |
| `backfillConfig` (seed historical evals) | yes | **no** |
| `description` | yes | **no** |
| `evaluationSchedule`, `mode`, `actions`, `executorId` | yes | yes |

## datahub_freshness_assertion

`UpsertDatasetFreshnessAssertionMonitorInput` fields: `entityUrn, description, inferWithAI, inferenceSettings, schedule, evaluationSchedule, filter, failureSeverityConfig, actions, evaluationParameters, mode, executorId`.

| Capability | API | Resource |
|---|---|---|
| Schedule type `CRON` | yes | yes |
| Schedule type `FIXED_INTERVAL` | yes | yes |
| Schedule type `SINCE_THE_LAST_CHECK` | yes | **no** |
| Source type (`DatasetFreshnessSourceType`): `FIELD_VALUE, INFORMATION_SCHEMA, AUDIT_LOG, FILE_METADATA, DATAHUB_OPERATION, PLATFORM_API` | 6 | passthrough (2 documented) |
| `filter` | yes | **no** |
| `inferWithAI` + `inferenceSettings` | yes | **no** |
| `failureSeverityConfig` | yes | **no** |
| `description` | yes | **no** |
| `evaluationSchedule`, `mode`, `actions`, `executorId` | yes | yes |

## datahub_sql_assertion

`UpsertDatasetSqlAssertionMonitorInput` fields: `entityUrn, description, type, inferWithAI, inferenceSettings, statement, changeType, operator, parameters, failureSeverityConfig, actions, evaluationSchedule, mode, executorId`.

| Capability | API | Resource |
|---|---|---|
| Sub-type `METRIC` | yes | yes |
| Sub-type `METRIC_CHANGE` (+ `changeType` ABSOLUTE/PERCENTAGE) | yes | **no** |
| Operators (`AssertionStdOperator`, 18 values) | 18 | 6 |
| `statement`, `value`, `description` | yes | yes |
| `inferWithAI` + `inferenceSettings` | yes | **no** |
| `failureSeverityConfig` | yes | **no** |
| `evaluationSchedule`, `mode`, `actions`, `executorId` | yes | yes |

## Cross-cutting gaps (all three monitor types)

- `inferWithAI` + `inferenceSettings` -- AI/smart thresholds, a headline DataHub Observe feature -- modeled by none.
- `failureSeverityConfig` -- per-severity failure behaviour -- modeled by none.
- The `*_CHANGE` half of volume and sql, and the `SINCE_THE_LAST_CHECK` freshness schedule -- not modeled.
- `MonitorMode` is `ACTIVE, INACTIVE, PASSIVE`; resources document `ACTIVE`/`PASSIVE` (passthrough, so `INACTIVE` is usable).
- `filter` (volume, freshness) and `backfillConfig` (volume) -- not modeled.

## Interpretation

The typed resources cover the absolute-threshold, hand-authored common case well. The uncovered surface is concentrated in (1) the change/growth sub-types, (2) AI-inferred thresholds, and (3) finer evaluation controls (filter, severity, backfill). Some of this (smart/auto assertions) overlaps the "asset-level, not platform-config" boundary the provider intentionally avoids; some of it (growth assertions, severity) is squarely the kind of thing a platform team would want in code.

This matrix is the evidence base for the typed-vs-generic-vs-codegen decision in [assertion-resource-modeling.md](assertion-resource-modeling.md).
