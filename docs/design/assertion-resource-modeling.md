# Assertion resource modeling: typed vs generic vs codegen

Status: design note / decision record. Written after a brownfield migration field test ([extract-import-field-test.md](extract-import-field-test.md)) exposed how much of DataHub's assertion surface the current typed resources do not cover. Coverage evidence: [assertion-coverage.md](assertion-coverage.md).

## The problem

DataHub's `assertion` is a single entity whose `assertionInfo` aspect is a **tagged union over a large, Cloud-evolving set of types** (FRESHNESS, VOLUME, SQL, FIELD, DATASET, DATA_SCHEMA, CUSTOM, ...), and each type has sub-variants the platform keeps extending. The provider models a few of these as **strongly-typed resources** with firm schemas: `datahub_freshness_assertion`, `datahub_volume_assertion`, `datahub_sql_assertion`, plus `datahub_custom_assertion`.

Live introspection (see the coverage doc) shows those typed resources express roughly the absolute-threshold common case and omit, across the three monitor types: the `*_CHANGE`/growth sub-types, AI-inferred thresholds (`inferWithAI`/`inferenceSettings`), row `filter`s, `failureSeverityConfig`, `backfillConfig`, and `SINCE_THE_LAST_CHECK`. For `datahub_volume_assertion` that is roughly half of the volume-assertion UI.

This is structural: **a typed wrapper over an extensible union always lags the union.** Every new operator/sub-type/parameter DataHub ships requires a provider schema change and release. Continuing to hand-add typed assertion resources commits us to that treadmill indefinitely.

## Three options

### A. Hand-typed resources (current)
- **Pros:** best UX -- typed attributes, enum validation, plan-time errors, self-documenting HCL, first-class references (`entity_urn`).
- **Cons:** chases every enum forever; perpetually behind; one shared entity type fanning out to four resources also breaks the extract registry's one-entity-one-resource model, so monitor assertions cannot be auto-enumerated (they import by URN only).

### B. Generic `datahub_assertion` (raw structured JSON)
A resource taking a JSON `definition` mirroring the API, stored and round-tripped via `jsontypes.Normalized` -- the `aws_iam_policy` / `datahub_ingestion_source.recipe` pattern.
- **Pros:** covers every type and future addition with no provider change; enumeration becomes trivial (one entity, one resource, one enumerator); escapes the treadmill.
- **Cons / why assertions are harder than recipes:**
  1. **Write API is type-segmented.** Unlike ingestion sources' single `createIngestionSource`, assertions are written via separate typed mutations (`upsertDataset{Volume,Freshness,Sql}AssertionMonitor`, `upsertCustomAssertion`). There is no single generic upsert, and writing raw aspects via OpenAPI v3 bypasses the monitor service layer (forbidden -- see the read/write rule in CLAUDE.md). So a generic resource still needs a small per-type **write-dispatch** layer.
  2. **Input shape != stored/read shape.** One write input fans out into two entities and three aspects on read (`assertionInfo` + `assertionActions` on the assertion, `monitorInfo` on a separate Monitor entity -- the split that forced `GetAssertionMonitor`). So `Read` must **assemble and re-nest** an input-shaped document from multiple sources, not pass a blob through. (Worked example in the normalization section below.)
  3. **AI inference breaks round-trip.** With `inferWithAI = true`, the server computes the thresholds and stores concrete numbers the user never wrote, so the read document diverges from config unless the resource tracks "inferred" as separate intent state.
- **Net:** still the best coverage answer, but it is "thin write-dispatch + a read-normalization mapper + an inference carve-out", not a dumb blob.

### C. Schema-driven codegen
Introspect DataHub's GraphQL schema in CI and generate the typed inputs/resources.
- **Pros:** keeps typed UX while making "keep up" mechanical (regenerate + release).
- **Cons:** upfront harness; must run against a **dumped SDL**, not live per-plan, because of the `BadFaithIntrospection` guard; codegen produces the input mapping but cannot infer the read-normalization (how the service splits an input into aspects) -- that stays hand-written.

## Normalization sketch (why B is not a pass-through)

For a demo volume assertion, the generic resource's `Read` reconciles:

```
definition (write input)                     <= read source
type: "ROW_COUNT_TOTAL"                       <= assertion / assertionInfo.volumeAssertion.type
rowCountTotal { operator, parameters }        <= assertion / assertionInfo.volumeAssertion.rowCountTotal
entityUrn                                     <= assertion / assertionInfo.entityUrn
actions                                       <= assertion / assertionActions aspect
evaluationSchedule { cron, timezone }         <= MONITOR / monitorInfo.assertionMonitor.assertions[*].schedule
evaluationParameters { sourceType }           <= MONITOR / monitorInfo...assertions[*].parameters.datasetVolumeParameters
mode                                          <= MONITOR / monitorInfo.status.mode
```

Steps: fetch two entities (assertion + its Monitor via the `Evaluates` link), merge three aspects, re-key/re-nest into the write-input vocabulary, then canonicalize (drop server defaults/nulls) so the assembled JSON semantically equals the user's config and `jsontypes.Normalized` suppresses formatting noise.

## Recommendation

A hybrid, in priority order:

1. **Keep the typed resources** for the common, stable cases -- they are the right ergonomic front door for hand-authored freshness/volume/sql monitors.
2. **Add type-routed enumerators** (FRESHNESS/VOLUME/SQL -> their resources, filtered on `info.type`, skipping shapes the resource cannot model) so the typed resources auto-extract like ingestion sources. This closes the migration enumeration gap with no design change. (Reliable: `info.type` cleanly discriminates the types -- verified live.)
3. **Add a generic `datahub_assertion`** (raw `definition` JSON, `jsontypes.Normalized`, per-type write-dispatch, the read-normalization above) as the **escape hatch** for everything the typed resources do not model -- growth/change sub-types, AI inference, filters, severity. `datahub_custom_assertion` is already a narrow instance of this idea; generalize it.
4. **Revisit codegen (C)** only if assertions become strategically central enough to justify the harness; introspection makes it feasible, run against an SDL dump.

Do not keep adding a fourth, fifth, sixth typed assertion resource to chase the union -- that is the treadmill this note exists to stop. New coverage should land via (3), with (2) making what we already have fully migratable.
