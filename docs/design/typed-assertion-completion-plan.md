# Typed assertion completion plan

Status: build record. Tracks the work to complete the existing typed assertion resources (`datahub_volume_assertion`, `datahub_freshness_assertion`, `datahub_sql_assertion`) to full NATIVE coverage of their type, per the decision in [assertion-resource-modeling.md](assertion-resource-modeling.md) (complete the typed resources rather than build a generic). Evidence base: [assertion-coverage.md](assertion-coverage.md). Scope rule throughout: **NATIVE source only** -- EXTERNAL (ingested) and INFERRED (AI/smart) assertions stay out of scope and are already filtered on enumeration and refused on import (PR #58).

## The reusable per-field pattern

Each increment repeats the same shape:

1. **Resource** -- add the attribute(s) and any plan-time validation.
2. **Client** (`internal/provider/pkg/datahub/assertions.go`) -- extend the `*AssertionInput`, send the field on upsert, add the read struct, populate `AssertionInfo` in `toAssertionInfo`.
3. **Enumeration** (`assertions_list.go`) -- widen the `List*AssertionURNs` keep-predicate when a new sub-shape becomes manageable.
4. **Mock** (`internal/provider/datahubtesting/assertions.go`) -- store and echo the field.
5. **Tests** -- client read-parse, resource lifecycle + import, validation, enumeration inclusion.
6. **Docs** -- resource schema description + the import guide; regenerate.
7. **Empirical first** -- for any shape whose read JSON has not been seen, create + inspect + delete a throwaway NATIVE assertion on a live Cloud instance to capture the exact `assertionInfo.value` shape before coding.

## Completed increments

| Increment | Resource(s) | Notes |
|---|---|---|
| Volume `ROW_COUNT_CHANGE` | volume | `change_type` (ABSOLUTE/PERCENTAGE) required-when-CHANGE; reuses operator/value. Threshold nests under `volumeAssertion.rowCountChange`. |
| SQL `METRIC_CHANGE` | sql | `change_type` is a top-level sibling in both write input and read shape (unlike volume's nested object). METRIC_CHANGE additionally requires a non-empty `description` (server rejects otherwise). |
| Freshness `SINCE_THE_LAST_CHECK` | freshness | Third schedule type, no window sub-config. Added a schedule_type/window pairing validator; ImportState sets only the sub-fields belonging to the schedule type. |
| `description` | volume, freshness | Top-level `assertionInfo.description`; sql already had it. |
| `filter_sql` | volume, freshness | `DatasetFilter` -- `DatasetFilterType` has a single member (`SQL`), so the resource takes just the clause string. Reads from the type-nested `*.filter`. SQL has no filter input. |
| `failure_severity` | freshness, sql | `failureSeverityConfig.defaultSeverity` (LOW/MEDIUM/HIGH). Field exists on freshness/sql inputs only, NOT volume. Conditional `rules` engine not modeled. |
| `backfill_start_date_ms` / `backfill_state` | volume | `AssertionMonitorBootstrapConfigInput { backfillStartDateMs }`. Seeds the metrics cube so a new assertion has history to judge against. See below. |

## `backfillConfig`: why the original deferral was wrong

This was deferred on the finding that the value does **not** round-trip on read, which would have left a managed attribute in perpetual drift. It was later unparked on the theory that the WriteOnly pattern -- shipped four times by then -- was the missing prerequisite, since a WriteOnly attribute is never stored in state and never drift-compared.

**Both readings were wrong, and re-measuring is what settled it.** Four probes against DataHub Cloud (v1.7.x, August 2026), on a throwaway dataset:

| Probe | Result |
|---|---|
| Set on create | Persisted and readable at `monitorInfo.value.assertionMonitor.bootstrapConfig.backfillStartDateMs`, byte-identical to what was sent. Bootstrap status `PENDING`. |
| Omit on update | Config **cleared**; bootstrap status `PENDING -> REJECTED`. |
| Re-provide, different date | New value readable; status `REJECTED -> PENDING`. |
| 400 days back | Refused: *"backfillStartDateMs exceeds the maximum lookback of 365 days for non-bucketed assertions."* |
| Never requested at all | `bootstrapConfig` absent **and** the `monitorBootstrapStatus` aspect absent entirely -- not a `REJECTED` status. |

So it round-trips on exactly the path the provider already reads (`GET /openapi/v3/entity/monitor/{urn}`), and set/clear/change behave as an ordinary declarative attribute. A plain `Optional` Int64 is sufficient: no WriteOnly, no `_wo_version` companion, no drift.

Two consequences worth recording:

- **The re-trigger question answers itself.** The deferral asked whether replaying a backfill is rejected or silently re-runs, and treated it as the one thing the WriteOnly pattern could not settle. Changing the timestamp re-triggers, so the value is its own version marker -- which is precisely the job `_wo_version` would have been added to do.
- **Volume-only is load-bearing, not incidental.** The *field* assertion resolver rejects `backfillConfig` on anything that is not a smart (INFERRED) assertion; the volume resolver has no such guard and accepted it on a NATIVE assertion. This provider is NATIVE-only, so volume is the sole type where the attribute can exist at all.

The lasting lesson is narrower than "the finding was stale": an empirical result was recorded without the server version it was measured against, so nothing signalled when it expired. Re-measuring cost ten minutes; building on the record would have shipped a WriteOnly attribute solving a drift problem that no longer existed.

## Deferred (investigated, deliberately not built)

- **`failureSeverityConfig.rules`** (freshness, sql). Conditional per-result severity escalation (each rule = severity + operator + parameters). Niche, and the resource would have to own the full ordered list. Deferred behind `defaultSeverity`.
- **`inferWithAI` / `inferenceSettings`** (all). Setting `inferWithAI` flips the assertion to `source = INFERRED`, out of scope by the source rule. Never to be modeled on the typed resources. (A separate future `user-declared AI monitor` capability is noted in the modeling doc.)
- **Exclusion windows.** Surface under AI `adjustmentSettings` on the monitor; tied to the INFERRED path, hence out of scope alongside `inferWithAI`.

## New assertion types (Phase 5)

These are *new* assertion types, not "complete the existing types". The field-test instance had **zero** NATIVE FIELD/DATA_SCHEMA assertions (101 assertions: 96 EXTERNAL dbt DATASET tests + 5 NATIVE freshness/volume/sql), so they were initially gated on demand. Built anyway as forward-looking surface after confirming the read shapes empirically (create + inspect + delete throwaway assertions of each type on a live Cloud instance):

- **`datahub_field_assertion`** (FIELD) -- **built.** Models both sub-types: `FIELD_VALUES` (per-row value check, with optional `transform_type`, `fail_threshold_*`, `exclude_nulls`) and `FIELD_METRIC` (aggregate column metric over 17 `metric` choices). The column is a `{field_path, field_type, field_native_type}` spec that round-trips as a plain std type (unlike schema fields). FIELD_VALUES requires a warehouse-backed platform (bigquery/snowflake/redshift/databricks) and a query `source_type`; FIELD_METRIC works against a DatasetProfile.
- **`datahub_schema_assertion`** (DATA_SCHEMA) -- **built.** Owns the complete expected `fields` list and a `compatibility` mode (EXACT_MATCH / SUPERSET / SUBSET). On read the field std type comes back as a `SchemaFieldDataType` class object (`type.type.{com.linkedin.schema.NumberType:{}}`), so the client maps the class back to the std type (NumberType -> NUMBER).

### DATA_JOB_RUN freshness -- deferred

`FreshnessAssertionType` has `DATASET_CHANGE` (what `datahub_freshness_assertion` models) and `DATA_JOB_RUN`, which asserts a **DataJob** ran within a window. Deferred, because it is genuinely different surface on an unsuitable mutation:

- It targets a DataJob URN, not a dataset.
- The only mutation that accepts `type: DATA_JOB_RUN` is `createFreshnessAssertion` (entityUrn, schedule, filter, failureSeverityConfig, actions) -- a **create-only** assertion mutation with **no monitor** (no evaluationSchedule / mode / source / executor). The dataset monitor upsert (`upsertDatasetFreshnessAssertionMonitor`) is dataset-only and does not accept DATA_JOB_RUN.
- Create-only with no upsert means no clean update path, and no monitor means a different resource shape from the existing dataset freshness resource.
- Zero observed usage on the field-test instance.

Revisit if a real DataJob-run freshness need appears; it would be a separate `datahub_data_job_freshness_assertion`-style resource, not an extension of `datahub_freshness_assertion`.

## Generic backstop (Phase 6)

A generic `datahub_assertion` (raw JSON) remains an optional future backstop for shapes deliberately not typed, not the primary coverage mechanism. Out of scope unless a concrete need for an unmodeled NATIVE shape appears.
