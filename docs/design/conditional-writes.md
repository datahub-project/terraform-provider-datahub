# Conditional writes: protecting read-modify-write against lost updates

Status: shipped with the `If-Version-Match` change (see CHANGELOG `[Unreleased]`). Probe answers recorded 2026-11-03 against Quickstart v1.7.0 and a DataHub Cloud instance.

## The problem

An OpenAPI v3 aspect write replaces the whole stored aspect. A writer that manages some of an aspect's fields therefore reads the aspect, sets its fields on what it read, and writes the result back, so the fields it does not model survive (`dataProductProperties.assets`, `glossaryTermInfo.sourceRef`, `corpUserInfo.managerUrn`, and so on). That read-modify-write has a window. Anything written to the aspect between the read and the write, by the DataHub UI, the CLI, an ingestion run, SCIM, or another `terraform apply`, is overwritten by the provider's write, with HTTP 200 and no trace. The per-entity keyed mutex the provider has carried since 0.15.0 for structured property assignments protects only against the provider's own goroutines in one process.

The DataHub platform team named this provider as the read-modify-write case and gave the supported cross-process fix: read the aspect with its `systemMetadata.version`, write with `If-Version-Match`, and treat the resulting hard failure as the signal to re-read and retry. This document records the survey of every alternative, the mechanism as built, what the live probes established, and what remains.

## How DataHub's conditional writes work

Every stored aspect carries `systemMetadata.version`: `"1"` on its first write, advanced by one on every write that changes the value, unchanged by a write whose body is identical. It is a decimal string and is compared as a string. A hard delete followed by a recreate restarts at `"1"`.

A read with `?systemMetadata=true` returns it per aspect, and `?aspects=a,b` narrows one GET to several aspects. The provider never echoes the rest of `systemMetadata` back: the server keeps a sent `runId`, which would tie a Terraform write to whatever ingestion run last touched the aspect and make that run's rollback revert it.

A write carries the precondition in the per-aspect `headers` object of the collection POST body, not as an HTTP header (the v3 controller never copies HTTP headers into the change):

```json
[{"urn": "urn:li:corpuser:jdoe",
  "corpUserKey":  {"value": {"username": "jdoe"}},
  "corpUserInfo": {"value": {"...": "..."}, "headers": {"If-Version-Match": "3"}},
  "subTypes":     {"value": {"typeNames": ["SERVICE_ACCOUNT"]}}}]
```

`ConditionalWriteValidator` (shipped in DataHub v0.14.0, always on, byte-identical in the Cloud fork) compares the header to the stored version inside the write transaction, after the locking read. On a mismatch it raises a `PRECONDITION` validation exception, the whole request rolls back (one transaction per batch, so a mix of valid and mismatched aspects persists nothing), and the v3 controller answers HTTP 412 (`{"error":"Precondition Error","message":"... msg=Expected version 3, actual version 4)..."}`; the 412 mapping arrived in v1.0.0, before which the same failure surfaced as a 500). `"-1"` is the version of an aspect that does not exist, so `If-Version-Match: -1` means "must not exist yet", and it is race-safe: two creators racing for one URN get one success and one 412. `?async=false` is mandatory, because the default routes the batch through Kafka, where a 412 lands in the failed-MCP topic and HTTP says 200.

### What the live probes established

`TestLiveConditionalWriteProbe` (`internal/provider/pkg/datahub/live_conditional_write_test.go`) runs in every live acceptance run and answered the following identically on Quickstart v1.7.0 and on DataHub Cloud:

| Question | Answer |
|---|---|
| GET of an absent entity, or of an existing entity with `?aspects=` naming an aspect it lacks | 404 in both cases; both map to `"-1"` |
| `?aspects=a,b` on one GET | works; one read per write, however many aspects it preserves |
| First conditional write with `"-1"` | 200; the aspect is then at `"1"`; the POST response carries the new version when the POST URL also has `systemMetadata=true` |
| Mismatch | 412 with the body above; the rejected write did not persist and the version did not move |
| `"-1"` on an existing aspect | 412 (atomic create-if-absent) |
| Identical body with the correct version | 200, version unchanged; a changed body advances it by one |
| `If-Unmodified-Since` | works with an ISO-8601 instant; an RFC-1123 HTTP date is a 500 |
| Soft-deleted entity (`status.removed = true`) | the v3 GET still returns its aspects and versions, with or without `includeSoftDelete`; `"-1"` on it is a 412 |
| 20 sequential read-then-conditional-write cycles | 0 spurious 412 on either target |
| Cloud differences | none in behaviour; the 412 message is prefixed `Failed to validate MCP due to: ` and reads carry extra `aspectCreated`/`aspectModified` stamps |

## The options surveyed

Write shapes in this provider: (1) v3 collection POST after a client-side read (the nine writers in `aspectWriters`); (2) v3 JSON PATCH (unused today); (3) GraphQL mutations that read-modify-write server-side (`upsertStructuredProperties`, `addRelatedTerms`, `batchAddOwners`, `addGroupMembers`, `updateOrganizationDisplayPreferences`, `deleteOAuthAuthorizationServer`); (4) GraphQL mutations that rebuild a whole aspect from their input (`createForm`, `updatePolicy`, `upsertActionPipeline`, page templates and modules, data contracts, tests, connections, OAuth servers, executor pools, secrets, assignment rules, assertion monitors, `batchAssignRole`).

| Option | Covers | A lost update becomes | Failure visibility | Verdict |
|---|---|---|---|---|
| `If-Version-Match` per-aspect body header on the v3 upsert, read with `systemMetadata=true`, bounded re-read and retry | (1), and any future client-side read-modify-write | impossible: the write lands on exactly the version read or not at all, and a batch is one transaction | hard, 412 naming expected and actual | adopted |
| `If-Unmodified-Since` / `If-Modified-Since` | (1) | less likely: millisecond granularity, clock skew between GMS nodes, `lastObserved` is not monotonic | hard on mismatch; 500 on a non-ISO value | rejected, strictly weaker on the same validator |
| ETag / `If-Match` / `If-None-Match` HTTP headers | nothing: DataHub emits no ETag or Last-Modified and reads no conditional HTTP header; the only `If-None-Match` handling is a body header on CREATE that silently drops the conflicting write | none | silent by design | rejected; recorded so nobody reaches for it as an atomic create (that is `If-Version-Match: -1`) |
| v3 JSON PATCH, keyed arrays, applied inside the server's write transaction | (2); the migration target for (3) where the list has a primary key (`structuredProperties` by `propertyUrn`, `ownership` by `owner` plus `typeUrn` via `arrayPrimaryKeys`) | impossible for keyed add and remove | hard but opaque (datahub-project/datahub#18935) | adopted as the follow-on for those two aspects; not for `glossaryRelatedTerms` or `nativeGroupMembership`, whose lists are primitive URN arrays with no key; note the generic patch path drops body `headers`, so a PATCH can never also carry `If-Version-Match` |
| GraphQL `patchEntity` with `headers` (v1.4.0) | same as PATCH | same as PATCH | 200 with `success: false` | rejected in favour of v3 PATCH |
| Attribution-keyed array elements | `documentation` only (the one template keyed on `attribution.source`) | n/a elsewhere | n/a | not a mechanism for the provider's writers; attribution stays provenance |
| Client-side keyed mutex (the status quo for three GraphQL paths) | (3), same process only | less likely | silent across processes | kept as an interim guard on server-side read-modify-write mutations only; never on a v3 path |
| `-parallelism=1` or documentation | in-apply ordering only | none across processes | silent | rejected; the control arm of a live probe only |
| Write-then-verify read-back (`SetGlobalTags`) | any | detectable in one direction; cannot see the write it just overwrote | hard, for the wrong reason | kept for silent-no-op detection (CAT-2562); not lost-update protection |
| Server-side `OPTIMISTIC_LOCKING_ENABLED` (v1.7.0.1+, default off) | server internal | unchanged for a stale client | n/a | irrelevant to the client; `If-Version-Match` stays sound with it on |
| `-1` create-if-absent | every v3 check-then-create | impossible to double-create or overwrite an entity that appeared in the window | hard, 412 | adopted; replaces the structured property exists-check |
| Rest.li `ingestProposal`, OpenAPI v2, a version stored in Terraform state | alternatives to the first row | as the first row or worse | 422, or stale under `-refresh=false` | rejected |

**What stays uncovered.** The whole-replace GraphQL mutations in shape (4) accept no precondition; the server rebuilds the aspect from the input and writes it unconditionally. The one worth moving is `dataHubPolicyInfo`: the provider already reads it via v3, and a conditional v3 write through the helper would carry `actors.roles` verbatim, delete the policy write guard, and enable managing roles (CAT-3192). The page-template backup write is the second candidate (a provider-private entity). Forms stay on `createForm`. A version read-back around a GraphQL write is deliberately not built: it cannot tell a lost write from a late one, a no-op leaves the version unchanged, and it doubles the request count.

## The mechanism as built

`internal/provider/pkg/datahub/aspect_write.go` is the one path for every OpenAPI v3 aspect write in the package. The design makes the version impossible to drop by construction, with three fences:

1. **The type.** `readPreservedAspects` is the only constructor of `preservedAspect`, a value holding an aspect's unmanaged fields and the version they were read at, and `preservedAspect.entry()` is the only way to turn one into a versioned entry of the write body. A writer declares the aspects it preserves in its `aspectWriteSpec` and receives them in a `mergeFunc` that sets its managed fields and returns the entries; `ownedEntry` is for aspects the writer replaces whole (key aspects, `globalTags`, `tagProperties`, `subTypes`, data product `domains`), which it did not read and which carry no precondition.
2. **The runtime fence.** `writeAspects` refuses a merge result that omits a declared aspect, returns it without a version, or returns it with a version other than the one just read, as a provider bug, before anything is sent.
3. **The guard tests.** `aspect_write_guard_test.go` runs every classified writer against a server that serves distinct versions, and asserts on the wire that every preserved aspect carries `If-Version-Match` equal to the version served and was read with `systemMetadata=true`, that no owned aspect carries one, that no aspect echoes `systemMetadata`, that an absent aspect is created with `"-1"`, that an aspect served without a stored version is written with `"1"` (the server's own resolution for rows written before 0.14), and that `postAspects` is the only function in the package that POSTs to the entity endpoint.

`writeAspects` reads all preserved aspects in one GET, merges, POSTs with `async=false`, and classifies the answer. A 412 whose message names the expected and actual versions (or a 500 carrying the validator's phrasing, for servers between 0.14 and 1.0) becomes a `VersionConflictError`; a 412 without them is surfaced as it is, because the server answers 412 whenever a precondition failure is among the validation failures and a real validation error riding along must not be retried into silence; the loop then re-reads, re-merges on fresh maps, and retries with full-jitter backoff, four attempts in all, before returning the conflict with the attempt count and the server's message. A 429 or 503 becomes a `RetryAfterError` and is waited out on its own budget, so back-pressure never consumes the conflict budget and is never reported as a conflict. A write declared `createOnly` reads first (with the `status` aspect, so an entity that already exists is reported before anything is sent, and as soft-deleted when that is why its id is taken), then sends `"-1"` on every preserved aspect so an entity that appears between the read and the write is still refused by the server; a 412 on a create is an `EntityExistsError` and is never retried. `datahub_structured_property` creates this way. The read is kept rather than relying on `"-1"` alone because a server predating the validator would ignore the header and overwrite. Every sleep honours context cancellation and the resulting error carries both the cancellation and the conflict it was retrying.

The diagnostic a practitioner sees after the retries are exhausted names the aspect and entity, the version read and the version held, how many retries were made, that nothing was written and `terraform apply` is safe to re-run, and what to look for (an ingestion run, a sync job or another Terraform run rewriting the aspect continuously).

Owned-whole aspects carry no precondition by design. Those writers do no read, so a just-in-time read would guard only the microseconds between it and the POST, not the refresh-to-apply window a "detect UI edits" mode would need; that needs the version carried in private state from Read to Update and is a separate design. On a 412 there would be nothing to merge, and Terraform's contract for owned fields is that plan shows the drift and apply overwrites it. `aspectEntry.version` is nil-able so that mode can be added later without touching the body builder.

### Minimum DataHub version and legacy rows

The validator and the body `headers` exist from DataHub v0.14.0 (Cloud v0.3.5); the clean 412 from v1.0.0 (Cloud v0.3.9). The provider does not compare version numbers and refuses nothing on version grounds. An aspect served without `systemMetadata.version` is one last written before 0.14, which happens on long-lived upgraded instances, not only on old servers; the validator resolves such a row to `max(1, rowVersion)` and the latest row is 0, so the provider sends `"1"`, which is exactly what the server will compare against. On a server with no validator at all the header is ignored, so nothing is lost there either; between 0.14 and 1.0 a conflict arrives as a 500 that the provider recognises by its message.

## How it is tested

- **Guard tests** (`pkg/datahub/aspect_write_guard_test.go`): the on-the-wire assertions above, for every writer in `aspectWriters`, which an AST scan keeps complete in both directions.
- **Conflict tests** (`pkg/datahub/aspect_write_conflict_test.go`): a forced 412 surfaces as the typed error after exactly four reads and four writes; a one-shot conflict is retried with a fresh read, and the retry carries the competing edit, which proves the merge was redone rather than the stale write replayed; a perpetual conflict is bounded and never replays a rejected version; a conflict on one of the two structured property aspects retries the whole batch with both versions re-read; a 503 is a `RetryAfterError`, never a conflict, and keeps the header on every attempt; create-only reads once (with `status`), reports an existing entity before any write and names it soft-deleted when it is, then POSTs with `"-1"` and never retries a 412; a 412 that names no versions is not a conflict; cancellation interrupts the backoff; the runtime fence rejects an owned entry for a preserved aspect, an omitted preserved aspect and a wrong version; the body never carries `systemMetadata`.
- **Mock version layer** (`datahubtesting/aspect_versions.go`): the acceptance mock gives every aspect a version, enforces `If-Version-Match` with the server's 412 body and whole-batch rollback, confirms presence on every check so a deleted entity reads as absent, treats an aspect the typed handlers serve but no POST created as the server's fallback version 1, and keeps a ledger of every v3 POST. `/test-control/arm-aspect-conflict` stands in for a competing writer: after the provider's next read-for-write of the aspect, the version advances and a field appears. `TestAcc_UnmanagedAspectFieldsSurviveUpdate` uses it for each of the ten resource cases, asserting the write carried the version it read, that the conflict was met and recovered, and that both the competing edit and the seeded unmanaged field survived.
- **Live tests** (`internal/provider/aspect_version_live_test.go`): an intercepting proxy (`datahubtesting/live_proxy.go`) sits between the provider and a real DataHub and runs a competing conditional write at the only moment that matters, after the server has served the provider's read-for-write and before the provider sees it, so nothing depends on timing. `TestLiveConditionalWriteRace` drives the client directly and `TestAcc_LiveConditionalWriteSurvivesConcurrentEdit` drives the provider; both assert the provider's write met a 412, retried, and both edits survived. Each has a control arm that strips the precondition from the POST and asserts the competing edit is **lost**: the standing proof that the harness detects the hazard it exists for. If the control ever passes without the header, the server has started merging and this design needs re-examining.
- **Proven to bite.** Before the writers were migrated, the guard tests failed for all nine preserved aspects ("carried no If-Version-Match", "sent If-Version-Match \"\" want \"-1\"", "carried no version but the writer wrote anyway"), and the acceptance scenario failed on the ledger and the lost competing edit.

## Follow-ons

- The CAT-2568 keyed mutex on structured property assignments, `relatedTermLocks` and `globalSettingsLocks` stay: they guard GraphQL mutations that read-modify-write server-side, which no client precondition can reach. The exit for structured property assignments and entity ownership is v3 JSON PATCH keyed on the element's primary key. The exit for related terms and group membership (primitive URN arrays, no key, no template) is a conditional v3 read-modify-write through `writeAspects`, with the resolvers' referential checks reproduced client-side.
- `SetOrganizationDisplayPreferences`, `BatchAddOwners`/`RemoveOwner` and `AddGroupMember`/`RemoveGroupMember` are server-side read-modify-writes without a lock; they get interim keyed mutexes in a separate change.
- Policy: move `UpsertPolicy` from `updatePolicy` to a conditional v3 write of `dataHubPolicyInfo`, deleting the write guard and enabling `actors.roles` (CAT-3192).
- Page template backup: write the backup entity through v3 with `"-1"` on its properties aspect, copying the live aspect as opaque JSON.
