# Build scope: `ROW_COUNT_CHANGE` (growth) support in `datahub_volume_assertion`

First increment of "complete the existing typed resources to full NATIVE coverage" (see [assertion-resource-modeling.md](assertion-resource-modeling.md), recommendation item 1). Adds the volume *change/growth* sub-type alongside the existing `ROW_COUNT_TOTAL`, so `datahub_volume_assertion` covers all NATIVE volume assertions -- and "all volume in one resource" holds without a generic.

This is the UI's "Table row count is **growing by at most / at least / within a range**" -- the three options the resource cannot express today.

## Verified API shapes (live introspection + a created-and-deleted test assertion)

Write input `UpsertDatasetVolumeAssertionMonitorInput.rowCountChange` (`RowCountChangeInput`):

```
type:       AssertionValueChangeType   # ABSOLUTE | PERCENTAGE   (required)
operator:   AssertionStdOperator       # BETWEEN, GREATER_THAN_OR_EQUAL_TO, ... (required)
parameters: AssertionStdParametersInput # value | minValue/maxValue (same shape as rowCountTotal)
failureSeverityConfig: ...             # optional, not modelled yet
```

Read (assertion entity `assertionInfo.value.volumeAssertion`) for a NATIVE change assertion -- confirmed:

```json
{ "type": "ROW_COUNT_CHANGE",
  "entity": "urn:li:dataset:(...)",
  "rowCountChange": { "type": "ABSOLUTE",
                      "operator": "GREATER_THAN_OR_EQUAL_TO",
                      "parameters": { "value": { "type": "NUMBER", "value": "10" } } } }
```

Key facts confirmed: an explicit-threshold change assertion is **`source = NATIVE`** (in scope); the discriminator is `volumeAssertion.type`; the parameters reuse the same `AssertionStdParameters` shape as total (so `BETWEEN` -> min/max, others -> single value -- the existing `min_value`/`max_value`/`single_value` attributes map directly). Monitor-side fields (evaluation schedule, source type, mode) are unchanged.

## Resource schema (`internal/provider/volume_assertion_resource.go`)

- `volume_type`: accept `ROW_COUNT_CHANGE` in addition to `ROW_COUNT_TOTAL` (doc + validator).
- New `change_type` attribute: `ABSOLUTE` | `PERCENTAGE`. **Required when** `volume_type == "ROW_COUNT_CHANGE"`, and must be absent/null otherwise (validate both directions).
- Reuse `operator`, `min_value`, `max_value`, `single_value` unchanged (same parameter shape).
- Backward compatible: existing `ROW_COUNT_TOTAL` configs are untouched (`change_type` optional, null for them).

## Client (`internal/provider/pkg/datahub/assertions.go`)

- `VolumeAssertionInput`: add `ChangeType string`.
- `UpsertVolumeAssertion`: currently always nests params under `rowCountTotal`. Branch on `in.VolumeType`: build `rowCountChange { type: in.ChangeType, operator, parameters }` for `ROW_COUNT_CHANGE`, else `rowCountTotal` as today.
- Read shapes: `assertionInfoValue.VolumeAssertion` add a `RowCountChange` struct (`type`, `operator`, `parameters{value,minValue,maxValue}`); `VolumeAssertionInfo` add `ChangeType string`. `toAssertionInfo`: populate from `rowCountChange` when present (set `VolumeType=ROW_COUNT_CHANGE`, `ChangeType`, operator, min/max/value).

## Enumeration + import (already mostly there)

- `ListVolumeAssertionURNs` (`assertions_list.go`): widen the keep predicate from `VolumeSubType == "ROW_COUNT_TOTAL"` to `{ROW_COUNT_TOTAL, ROW_COUNT_CHANGE}`. `scanAssertions` already fetches `volumeAssertion.type`, so no query change.
- Import guard (`source==NATIVE`): unchanged -- NATIVE change assertions pass; INFERRED/EXTERNAL still refused.

## Mock (`internal/provider/datahubtesting/`)

- `handleUpsertVolumeAssertion`: currently stores `rowCountTotal` only; store `rowCountChange` when the input has it (keep the sub-type discriminator on `VolumeAssertion["type"]`).
- `buildAssertionEntityJSON` volume case: emit `rowCountChange` when present.
- `assertionSearchInfo` already surfaces `volumeAssertion.type`, and `/test-control/seed-assertion` already takes a `subType` -- so enumeration tests can seed `ROW_COUNT_CHANGE`.

## Tests

- Client round-trip (mock): upsert + read a `ROW_COUNT_CHANGE` (ABSOLUTE and PERCENTAGE; single value and BETWEEN).
- Resource lifecycle (mock + live-cloud): create -> update -> import a change assertion; ImportStateVerify (with the existing monitor-field ignore list).
- Validation: `change_type` required with `ROW_COUNT_CHANGE`, rejected with `ROW_COUNT_TOTAL`.
- Enumeration: a seeded NATIVE `ROW_COUNT_CHANGE` is now enumerated (extend the existing exclusion test).

## Docs

- `volume_assertion` schema description: `volume_type` is `ROW_COUNT_TOTAL` | `ROW_COUNT_CHANGE`; document `change_type` and the growth operators.
- Import guide supported-types row + the Assertions note: volume now covers both sub-types. Regenerate (`make generate`).

## Scope boundary

In: `ROW_COUNT_CHANGE` with explicit (NATIVE) thresholds. Out (separate items): AI-inferred change (that's INFERRED, out of scope); `failureSeverityConfig`, row `filter`, exclusion windows, `backfillConfig` (cross-cutting, separate increments); sql `METRIC_CHANGE` and freshness `SINCE_THE_LAST_CHECK` (sibling increments, same pattern). Estimated one focused PR.
