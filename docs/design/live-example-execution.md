# Live example execution (stage C)

Maintainer-facing design for running the runnable examples for real -- `terraform apply` then `terraform destroy` against a live DataHub Quickstart -- rather than only type-checking them. This is a design, not an implementation: nothing described here exists yet.

## Where this sits

Example testing in this repository has been built in stages, each closing a gap the previous one could not see.

| Stage | What it proves | Where it lives |
|---|---|---|
| A | Every registered resource and data source has a non-empty registry snippet | `TestRegistryExampleCoverage` |
| A1.5 | A published release actually starts, not merely downloads | `.github/workflows/nightly-live.yml:45-88` |
| B | Every `.tf` under `examples/` passes `terraform validate` against a freshly built provider | `internal/provider/example_validate_test.go`, run by `make test-examples` (`Makefile:184-187`) and CI (`.github/workflows/ci.yml:81-96`) |
| C | An example applied to a real instance creates what it claims, converges, and leaves nothing behind | this document |

Stage B is deliberately `validate` and not `plan`: the comment at `example_validate_test.go:20-27` records why. `validate` never reads a data source, so it covers the singular-lookup snippets a `plan`-based check would have to exempt wholesale. What `validate` cannot see is everything that only happens against a server: a `Create` that writes an aspect the `Read` cannot parse back, a `Delete` that silently leaves the entity in place, an attribute whose server-normalised value differs from the configured one and so produces a permanent diff. Those are the defects stage C exists to find.

Stage C is not a substitute for the acceptance suite. The acceptance tests already assert entity content resource by resource, against both a mock and a live Quickstart (`Makefile:104-110`, `.github/workflows/live-acceptance.yml`). Stage C asserts something the acceptance suite structurally cannot: that the *published configuration a user copies* is applyable and destroyable as written.

## Scope: all 19 runnable examples classified

Every directory under `examples/runnable/` falls into exactly one primary bucket. Several carry secondary blockers that would independently disqualify them; those are named so that a later reclassification (say, if a Cloud-capable target became available) does not have to rediscover them.

| Directory | Primary verdict | Why | Secondary blockers |
|---|---|---|---|
| `action-pipeline-dataplex-sync` | Cloud-only | `datahub_action_pipeline` (`main.tf:25`) and `datahub_action_pipelines` (`main.tf:50`) are both `cloudOnlyBadge` (`action_pipeline_resource.go:70`, `action_pipelines_data_source.go:42`); the `dataHubAction` entity type does not exist in OSS | README requires a pre-existing DataHub secret `GCP_SA_KEY` |
| `assertion-volume-sqlite` | Cloud-only | `datahub_volume_assertion` (`main.tf:45`) is `cloudOnlyBadge` (`volume_assertion_resource.go:87`); the monitor service layer is Cloud-only | Needs `fixtures/seed.py` run and a `datahub ingest` push before the assertion has a profile to evaluate |
| `connection-snowflake` | Runs on Quickstart | `datahub_connection` is `ossAndCloudBadge` (`connection_resource.go:132`); create is `upsertConnection` (`connections.go:107`) which stores an encrypted blob and never contacts Snowflake; delete is OpenAPI v3 precisely because the GraphQL mutation is absent in OSS (`connections.go:228-231`) | Three variables have no default (`variables.tf:13,18,35`); values are opaque to DataHub, so synthetic ones suffice |
| `connection-snowflake-ingestion-source` | Runs on Quickstart | Same as above plus `datahub_ingestion_source` (`main.tf:47`) | Same three variables; `connection_id` defaults to `prod-snowflake`, colliding with the previous example (see "Identifier collisions") |
| `data-product-simple` | Runs on Quickstart | Tag, structured property, domain and two data products, all `ossAndCloudBadge` | Two-phase: `var.enable_defaults` must be `false` for the first apply and `true` for the second (`variables.tf:1-4`), because provider configuration cannot depend on resources the same apply creates |
| `domain-simple` | Runs on Quickstart | Six `datahub_domain` resources in a three-level hierarchy | Nested destroy hits the child-domain race (see "Flakiness") |
| `executor-pool-basic` | Cloud-only | `datahub_remote_executor_pool` (`main.tf:19`) is `cloudOnlyBadge` (`remote_executor_pool_resource.go:64`) | README documents a 30-90 second `PROVISIONING_PENDING` to `READY` wait |
| `financial-services` | Too expensive or slow | Terraform reads two generated files (`main.tf:27`, `assertions.tf:22`), both produced by an in-directory `Makefile` that `pip install`s dependencies and shallow-clones a 40-60 MB repository (`Makefile:20-23`); README quotes 147 domains, 147 glossary nodes and 1500-2000 terms at full scale | Also Cloud-only: five assertion resources in `assertions.tf` are all `cloudOnlyBadge`. Also needs `ANTHROPIC_API_KEY` for `scripts/iso20022/auto_tag.py`. On a fresh checkout the `fileexists` guards make the plan empty, so a naive run would pass vacuously |
| `glossary-node-term-simple` | Runs on Quickstart | Four glossary nodes, four terms, all OSS-capable | Owns `urn:li:glossaryTerm:tf-example-revenue`, shared with `structured-and-custom-properties` |
| `ingestion-source-csv-enricher` | Runs on Quickstart | One `datahub_ingestion_source` (`main.tf:19`). The CSV URL at `main.tf:30` is fetched by the executor at ingestion time, not by Terraform, so apply needs no network | None |
| `ingestion-source-lookup` | Runs on Quickstart (conditional) | Read-only: one `data "datahub_ingestion_source"` (`main.tf:20`) | Depends on `datahub-gc` existing. `main.tf:16` asserts it is "present on every DataHub instance"; **I did not verify that a freshly booted Quickstart has finished creating it.** Gate on a preflight probe, or hold this example back until the claim is confirmed |
| `local-iam` | Runs on Quickstart | Group, native login, catalog user, three memberships, a role assignment and a policy, all `ossAndCloudBadge` | The heaviest risk profile of any in-scope example -- see "Flakiness" items 3 and 4 |
| `ownership-type-simple` | Runs on Quickstart | Two ownership types plus two lookups | Plural lookup is `listOwnershipTypes` (`ownership_types_list.go:38-44`), a GraphQL `list*` and therefore index-lagged |
| `provider-install-verification` | Runs on Quickstart | One `data "datahub_me"` (`main.tf:16`). Creates nothing | Overlaps the A1.5 registry smoke test, which already runs `validate` here. Useful in stage C as a credential preflight, not as coverage |
| `remote-executor-azure` | Too expensive or slow | README quotes roughly USD 280/month for two `Standard_D4s_v5` AKS nodes with billing starting at apply, and 10-15 minutes of provisioning. Already exempt from stage B (`example_validate_test.go:50-52`) | Also Cloud-only (`datahub_remote_executor_pool`, `datahub.tf:4`). Also needs three separate credentials (`variables.tf:1,11,17`) plus `az login` and a Cloudsmith entitlement |
| `secret-basic` | Runs on Quickstart | `datahub_secret` plus a referencing ingestion source | `value` is `Required` (`secret_resource.go:112`), and `var.secret_value` defaults to `null` (`variables.tf:5`), so the harness must pass `TF_VAR_secret_value` |
| `structured-and-custom-properties` | Runs on Quickstart | Glossary node and term, two structured properties, three assignments | Highest CAT-2583 exposure: it deletes structured properties that are assigned to entities the same destroy removes. `CHANGELOG.md:99` records this exact configuration producing husks |
| `structured-property-simple` | Runs on Quickstart | Two structured properties plus two lookups | Deletes structured properties (assigned to nothing, so a much narrower blast radius) |
| `tag-simple` | Runs on Quickstart | Three tags plus two lookups | `data.datahub_tags.all` is `searchAcrossEntities`-backed and eventually consistent (`tags_list.go:11-13`) |

Totals: **14 run on Quickstart**, **3 are Cloud-only**, **2 are too expensive or slow**.

The fourth bucket, **needs-external-credentials, is empty as a primary verdict**, and that is a finding rather than an oversight. Every example that needs a real third-party credential is already excluded by a stronger reason: `remote-executor-azure` on cost, `action-pipeline-dataplex-sync` on Cloud-only, `financial-services` on cost and Cloud-only. The two Snowflake examples look like members of the bucket but are not: DataHub stores the connection blob without ever dialling Snowflake, so synthetic values apply and destroy exactly like real ones.

## Opt-in mechanism

**Recommendation: a table in Go, in a new `internal/provider/example_live_test.go`, paired with a completeness test that fails the build when a directory is in neither the run-list nor the excluded-list.**

The mechanism that matters is not the allowlist. It is the completeness test beside it. This repository has already been bitten once by a coverage set that quietly failed to grow: `examples/provider` was validated by nothing until `TestEveryExampleIsValidated` was written to reconcile the file count, and the comment at `example_validate_test.go:196-202` records that the omission "was invisible until someone reconciled the file count by hand". Stage C reintroduces exactly that hazard at directory granularity. Whatever holds the opt-in list must therefore make an unclassified example a *failure*, not an absence.

The proposed shape, mirroring `exampleExemptions` (`example_validate_test.go:46-53`):

```go
// liveExamples are the runnable examples applied and destroyed against a live
// Quickstart, in the order they run. Order is load-bearing: see the
// identifier-collision section of docs/design/live-example-execution.md.
var liveExamples = []liveExample{
    {dir: "tag-simple"},
    {dir: "domain-simple", serialDestroy: true},
    {dir: "glossary-node-term-simple"},
    {dir: "structured-and-custom-properties", settleAfterDestroy: true},
    {
        dir:  "secret-basic",
        vars: map[string]string{"secret_value": "tf-example-live-harness"},
    },
    {
        dir: "data-product-simple",
        phases: []map[string]string{
            {"enable_defaults": "false"},
            {"enable_defaults": "true"},
        },
        settleAfterDestroy: true,
    },
    // ...
}

// liveExampleExclusions maps a directory under examples/runnable to the reason
// it is not applied live. Every directory must appear in exactly one of these
// two tables; TestEveryRunnableExampleIsClassified enforces that.
var liveExampleExclusions = map[string]string{
    "remote-executor-azure": "provisions billable Azure infrastructure " +
        "(roughly USD 280/month per its README) and is Cloud-only",
    "executor-pool-basic": "datahub_remote_executor_pool does not exist on " +
        "OSS DataHub",
    // ...
}
```

Why Go rather than the alternatives.

**Per-example marker file** (say `examples/runnable/<dir>/.livetest.hcl`) colocates the decision with the example, which is its one real advantage: the classification lands in the same diff as the example. Against it: an author who adds a directory and forgets the marker gets silence, which is the precise failure mode above -- a completeness check would have to walk for *absent* files and could not distinguish "not yet classified" from "deliberately excluded" without a second marker for exclusions, at which point the scheme is a two-file-per-directory allowlist scattered across 19 places. It also puts harness scaffolding inside directories users clone and copy.

**Makefile variable** (`make test-examples-live EXAMPLES="tag-simple domain-simple"`) is the cheapest to build and the worst to live with. Nothing is committed, so the real list ends up as a string in workflow YAML with no review surface, no place for the per-example reasoning, no way to express the two-phase apply that `data-product-simple` needs, and no completeness guarantee at all. It is worth keeping as an *override* -- `EXAMPLES=` filtering the Go table for a targeted local run -- but not as the source of truth.

The Go table has a further, decisive advantage: the harness needs the husk classifier and the target detection that already live in `internal/provider/datahubtesting` (`husk_diagnostic.go`, `target.go`). A Go test in the same module reuses them directly; a bash harness would reimplement `probeAspectShape` and its careful "found, but no aspects" case (`husk_diagnostic.go:150-153`) worse.

One export is required. `stillExistsAfterDestroyError` and `describeStillExists` are unexported (`husk_diagnostic.go:142,170`), and `Target.AssertEntityAbsent` (`target.go:155`) checks absence but does not classify a husk. The harness wants both: absence semantics plus the self-diagnosing message. Add an exported `datahubtesting.AssertURNAbsent(t, client, resourceType, urn)` composing the two, and have the existing `CheckDestroy` call sites keep using the unexported form.

## Identifier collisions and why serial execution is forced

Every runnable example uses fixed, human-readable identifiers by house convention (the `tf-example-` prefix rule in `CLAUDE.md`). Fixed identifiers make examples readable and cleanup obvious; they also mean two examples can name the same entity. Terraform has no idea the other configuration exists, so the result is a create that fails with "already exists", or -- worse, because it is silent -- two states both believing they own one entity, where the first `destroy` removes it and the second reports drift or a 404.

The starting hypothesis for this design named three collisions. Re-checking each against the tree, **only one of the three is real**, because a DataHub URN is namespaced by entity type and two examples sharing an id string do not share an entity unless the type also matches.

| Id string | Directories | URNs | Real collision? |
|---|---|---|---|
| `tf-example-revenue` | `glossary-node-term-simple/main.tf:66`, `structured-and-custom-properties/main.tf:57` | `urn:li:glossaryTerm:tf-example-revenue` in both | **Yes** |
| `tf-example-finance` | `domain-simple/main.tf:22`, `glossary-node-term-simple/main.tf:22` | `urn:li:domain:...` vs `urn:li:glossaryNode:...` | No |
| `tf-example-data-platform` | `domain-simple/main.tf:57`, `local-iam/main.tf:25` | `urn:li:domain:...` vs `urn:li:corpGroup:...` | No |

Extending the search past the `tf-example-` prefix, which the original list assumed, finds a second real collision that carries no prefix at all:

| Id string | Directories | URN | Real collision? |
|---|---|---|---|
| `prod-snowflake` | `connection-snowflake/variables.tf:4`, `connection-snowflake-ingestion-source/variables.tf:4` | `urn:li:dataHubConnection:prod-snowflake` in both | **Yes** |

A third near-miss is worth recording because it will become real the moment someone renames something: `domain-simple/main.tf:43` and `glossary-node-term-simple/main.tf:43` both use `tf-example-accounting`, and the display names `TF Example - Finance` and `TF Example - Accounting` are shared verbatim. Display names are not URN-bearing, so they collide harmlessly today.

Two real collisions is enough to force serial execution, and serial execution is the right answer even at one, because the alternative -- renaming ids so that no two examples ever agree -- pays a permanent readability cost in published documentation to buy a test-harness property. The published examples should keep saying `tf-example-revenue`.

**Serial with teardown between each** makes both collisions disappear: at most one configuration holds `urn:li:glossaryTerm:tf-example-revenue` at any instant, and the second example creates it fresh after the first has removed it. This is stronger than serial-then-teardown-at-the-end, which would still have the two configurations coexisting.

Proposed order, and why:

1. `provider-install-verification` -- creates nothing; fails in seconds if the PAT or GMS URL is wrong, before an hour of Quickstart time is spent.
2. `tag-simple`
3. `ownership-type-simple`
4. `domain-simple` -- nested destroy, gets the child-race treatment.
5. `glossary-node-term-simple` -- first owner of `tf-example-revenue`.
6. `structured-and-custom-properties` -- second owner of `tf-example-revenue`; must not overlap with 5.
7. `structured-property-simple`
8. `data-product-simple` -- two-phase apply.
9. `secret-basic`
10. `ingestion-source-csv-enricher`
11. `connection-snowflake` -- first owner of `prod-snowflake`.
12. `connection-snowflake-ingestion-source` -- second owner; must not overlap with 11.
13. `ingestion-source-lookup` -- read-only, and gated on the `datahub-gc` preflight.
14. `local-iam` -- **last**, deliberately. It is the only example that mutates an identity the harness itself authenticates as (see "Flakiness" item 4), so anything it breaks breaks nothing downstream.

Serial execution solves the collisions but does *not* by itself solve the asynchronous side effect described in "Flakiness" item 1: a structured-property delete in example 6 can land after example 7 has started. That is why the design adds an end-of-run sweep rather than relying on per-example checks alone.

## Assertions, in priority order

Four assertions per example, ordered by value per unit of maintenance. Everything below is generic -- none of it names an attribute of any resource -- which is what keeps stage C from becoming a second copy of the acceptance suite.

**0. Apply succeeds, destroy succeeds.** Implicit, and the single highest-value bit. A published example that does not apply is the defect stage C is chiefly hunting.

**1. `terraform plan` after apply proposes no resource changes.** This catches the whole drift and normalisation class -- a server that rewrites a value, a `Read` that maps an aspect back differently from how `Create` wrote it, a `Computed` attribute the provider forgets to set. It costs one extra plan.

Use `-detailed-exitcode` as the cheap first pass (0 means clean, 2 means changes). Do **not** treat exit 2 as the verdict. Several examples read plural data sources that are OpenSearch-backed and therefore index-lagged: `data.datahub_tags.all` (`tag-simple/main.tf:57`, backed by `searchAcrossEntities` per `tags_list.go:11-13`), `data.datahub_data_products.all` (`data-product-simple/main.tf:126`, `data_products_list.go:11-13`), `data.datahub_structured_properties.all` (`structured-property-simple/main.tf:64`), `data.datahub_ownership_types.all` (`ownership-type-simple/main.tf:45`, `listOwnershipTypes` per `ownership_types_list.go:38-44`). Between the apply and the plan the index catches up, the list grows, the output value changes, and the plan is non-empty for a reason that is not a provider defect. So on exit 2, re-run as `terraform plan -out=tfplan` plus `terraform show -json tfplan` and fail only if `resource_changes` contains an action other than `no-op`. Output-only churn is reported as a log line, not a failure.

**2. Destroy leaves nothing, checked through the strongly-consistent read path.** `GET /openapi/v3/entity/{lowercase-type}/{urn}` reads MySQL and is the only read the provider is permitted to trust for this (the rule and the `datahub_secret` incident that produced it are in `CLAUDE.md`). A GraphQL `list*` here would report "gone" for an entity that is merely unindexed, which is the failure mode this assertion exists to catch inverted.

Harvest the URNs from state immediately before destroy, via `terraform show -json` filtered to `values.root_module.resources[] | select(.mode == "managed")`, taking each resource's `urn` attribute. Filtering on `mode == "managed"` is what keeps the check honest: it automatically excludes `data.datahub_corp_user.member.urn`, which in `local-iam` resolves to `urn:li:corpuser:datahub`, an account the example must not delete and the harness must not expect to disappear.

Two gaps in that harvest, both worth naming:

- `datahub_ingestion_source` exposes no `urn` attribute -- only `id`, which is the `source_id` (`ingestion_source_resource.go:106`). Every other entity resource has one. The interim fix is a small type-to-URN-template map in the harness (`datahub_ingestion_source` -> `urn:li:dataHubIngestionSource:{id}`); the better fix is a follow-up adding the computed `urn` attribute for consistency.
- `datahub_corp_group_member`, `datahub_role_assignment` and `datahub_structured_property_assignment` are relationship resources with no entity of their own. Correctly skipped: their destroy removes an aspect edge, not an entity, and asserting entity absence would be asserting the wrong thing.

When a URN is still present, build the failure with the husk classifier rather than a bare "still exists". `describeStillExists` (`husk_diagnostic.go:142-165`) distinguishes a CAT-2583 resurrection (key and contentless aspects only) from a genuine delete failure (content aspects still present), and says which. That distinction is the difference between a five-minute triage and the weeks the comment at `husk_diagnostic.go:30-35` records losing to an unclassified nightly flake. Note that it never tolerates either shape -- a husk blocks re-creation just as effectively -- so this only sharpens the message.

**3. Outputs are non-empty.** Cheap, and it enforces the house rule that every runnable example must expose something the user can act on ("Outputs" in `CLAUDE.md`). `terraform output -json` must return at least one key and no key whose value is `null`. Do not additionally assert that any *particular* output has a particular shape; the index-lagged plural outputs above would make that flaky, and the value here is the smoke test, not the content.

**Deliberately excluded: entity-content assertions via the DataHub API.** Checking that `tf-example-pii` came back with description "..." duplicates `TestAcc_Tag_*` exactly, and doubles the maintenance cost of every schema change -- one edit in the acceptance suite, one more here, with the second one easy to forget and easy to let rot. Stage C's job is the configuration-as-published, not the resource semantics.

## Cleanup guarantees, and debris

**The strongest available guarantee comes from the target, not the harness: run against a throwaway Quickstart, booted and nuked per run.** `make testacc-quickstart` (`Makefile:104-110`) already does exactly this, with the teardown installed as a shell `EXIT` trap so it fires on failure, on error and on interrupt. Stage C should reuse that pattern verbatim. The consequence is that debris is bounded to one CI run and one container set: a destroy that fails midway leaves entities in a database that is deleted minutes later. No shared instance is polluted because there is no shared instance.

That is a deliberate rejection of the alternative. Pointing stage C at a long-lived instance (`make testacc-remote`) would give faster runs by skipping the 5-10 minute boot, at the cost of every failed destroy accumulating permanently -- and several examples degrade badly under accumulation. `local-iam` is the sharpest case: on OSS, DataHub's signUp guard rejects the request whenever the user entity exists at all, regardless of credentials (the table in `docs/design/local-user-login-oss-cloud-differences.md`), and the example uses a fixed email (`local-iam/variables.tf:15`). One leaked user makes that example fail forever on that instance. On an ephemeral Quickstart the same leak is invisible.

Within a run, the harness still owes:

- **Destroy always runs.** Registered as a `t.Cleanup` at the point the apply starts, so it fires even when the plan assertion fails, the harness panics, or the test times out.
- **One whole-configuration destroy retry.** The provider already retries the specific errors it can recognise -- `DeleteDomain` retries three times with linear backoff on "which has child domains" (`domains.go:344-368`) -- but it cannot see ordering problems that span resources. A single retry after a short settle handles the residue; more than one is masking a bug rather than tolerating a race. Flag examples that need it (`serialDestroy: true` in the table) so `-parallelism=1` is used for the retry, which is the known workaround for the nested-domain case.
- **Failure debris is reported, not silently dropped.** On a destroy failure, log every URN still present, each with its classified aspect shape, and upload the state file and the terraform logs as workflow artifacts. `live-acceptance.yml:38-52` already establishes the pattern for capturing diagnostics on failure only.
- **An end-of-run sweep.** After the last example, re-check every URN the whole run created. This is the only assertion that catches a late CAT-2583 resurrection: the side effect is asynchronous, so an entity that was provably absent when its own example finished can reappear while a later example runs. Report a URN that fails only the sweep distinctly from one that failed its own check -- the two have different causes and different fixes.

For anyone who does point the harness at a shared instance deliberately, the `tf-example-` convention is what makes a sweeper possible, and `Target.Name` (`target.go:130-135`) records the same reasoning for the acceptance suite's `tfprovider-` prefix. A sweeper is out of scope here; the ephemeral target removes the need.

## Where it runs, expected cost, and reporting

**A new reusable workflow `.github/workflows/live-examples.yml`, called from `nightly-live.yml` as a third job alongside `live-acceptance` and `registry-smoke-test`.**

Its own workflow, and its own Quickstart, rather than extra steps inside `live-acceptance.yml`. Three reasons. The destroy-leaves-nothing assertion is only meaningful on an instance whose contents the harness fully accounts for, and the acceptance suite leaves its own entities behind by design during a run. A failure in one job should not mask the other, and the two have completely different triage paths. And the acceptance suite's `timeout-minutes: 45` (`live-acceptance.yml:15`) is already sized for its own work.

Triggers: `schedule`, `workflow_dispatch`, and `pull_request` gated on the existing `run-live-ci` label -- matching the `if:` condition at `nightly-live.yml:34-38`, minus `push`. Excluding push-to-main is a deliberate trade: `live-acceptance` already boots a Quickstart on every merge, and booting a second doubles the cost of every merge to catch a narrower class of regression a few hours earlier. Promote it to `push` later if the measured wall-clock turns out to be small.

Expected wall-clock, all estimates -- **nothing here is measured, and the first implementation should report real numbers back into this section**:

| Phase | Estimate | Basis |
|---|---|---|
| Quickstart boot | 5-10 min | `BUILDING.md:113`; `QUICKSTART_HEALTH_TIMEOUT` defaults to 600s (`Makefile:20`) |
| Provider build | under 1 min | `make install` |
| Per example | 1-3 min | apply + plan + destroy + absence checks; `domain-simple` is the slow end because of the delete backoff, `provider-install-verification` the fast end at seconds |
| 14 examples | 15-40 min | |
| End-of-run sweep | under 1 min | |
| **Total** | **25-50 min** | |

Set `timeout-minutes: 75`. That leaves headroom for a slow image pull without letting a genuinely hung run occupy a runner for an hour beyond its budget.

Failure reporting: fail the job, name the example directory and the phase (apply, plan, destroy, sweep) in the first line of the error, and upload state plus logs as artifacts. Because the harness is a Go test, each example should be a `t.Run` subtest named for its directory, so the GitHub summary lists exactly which examples failed rather than one opaque failure.

**The release-window false positive does not apply to stage C.** The registry smoke test hits this: `make bump-examples` (`Makefile:137-150`) rewrites the version pin in every example during release prep, and that commit lands before the tag, so a scheduled run in that window fails because the pinned version is not yet on the Registry -- which is why `nightly-live.yml:71-75` spells the situation out in its own error message. Stage C is immune because it uses a dev override, exactly as stage B does (`example_validate_test.go:288-296`), and a dev override makes Terraform ignore `required_providers` entirely (the mechanism, and the way it can silently defeat a test, is documented at `target.go:22-38`).

I verified the load-bearing half of that claim rather than assuming it: with a dev override in `TF_CLI_CONFIG_FILE`, `terraform apply` in a copy of `examples/runnable/tag-simple` ran **without any `terraform init`** and reached the provider's credential check, failing only on the unreachable GMS URL it was pointed at. So the harness needs no init, no network and no Registry.

The corollary is worth stating plainly: because the pin is ignored, **stage C can never detect a wrong version pin**. That remains the job of `scripts/check-example-versions.sh`, wired into `.goreleaser.yml:7` as a pre-release gate, and of the registry smoke test. Stage C must not be read as covering it.

## What could make this flaky, and what to do about each

### 1. CAT-2583 structured-property resurrection

Deleting a structured property fires a server-side side effect that scrolls the eventually-consistent search index for entities carrying the property and JSON-PATCHes each hit; a patch landing on a concurrently hard-deleted entity resurrects it as a husk -- key aspect present, content aspect gone, invisible in the UI, still blocking re-creation with "already exists". The mechanism is documented at `structured_properties.go:522-536` and `husk_diagnostic.go:20-42`, and `CHANGELOG.md:99` records `structured-and-custom-properties` producing it in a single `terraform destroy`.

Three example directories are exposed: `structured-and-custom-properties` (properties assigned to entities the same destroy removes -- the worst shape), `data-product-simple` phase two (the marker property assigned to both products, then all three destroyed), and `structured-property-simple` (properties assigned to nothing, so narrow).

The provider already mitigates from both ends: `settleStructuredPropertyAssignments` waits for a zero-count streak before issuing the delete (`structured_properties.go:540-574`), and create-time husk repair removes a provably empty blocker and retries (`domains.go:118-135`, `glossary.go:170-183`). Neither is a guarantee, and the code says so: "cannot close it by construction -- the side effect runs asynchronously against a replicated index" (`structured_properties.go:534-536`).

Mitigations for the harness: mark these directories `settleAfterDestroy` and pause before the absence check; run the end-of-run sweep so a resurrection arriving after an example finishes is still caught; and classify every still-present URN with `describeStillExists` so the failure states which of the two shapes it is instead of leaving the next maintainer to reproduce it by hand. Do **not** tolerate a husk. It is a real failure -- it blocks the next apply -- and suppressing it here would hide the very upstream bug the classifier was written to make visible.

### 2. Nested-domain child-delete race

DataHub's child-domain guard queries OpenSearch. When a child is deleted and the parent delete follows immediately, as it does in a `terraform destroy`, the guard can fire spuriously on a stale index. `DeleteDomain` retries three times with linear backoff on exactly that error string (`domains.go:319-368`, citing datahub-project/datahub#17732).

`domain-simple` is a three-level hierarchy with four children across two parents (`main.tf:42-67`), so it hits the race by construction; `data-product-simple` creates one standalone domain and does not.

Mitigation: the in-provider retry handles most of it. Mark `domain-simple` `serialDestroy` so the harness's one destroy retry uses `-parallelism=1`, removing the concurrency that widens the window. Resist raising `maxRetries` in the provider to make CI green -- that changes user-facing behaviour to serve a test.

### 3. `datahub_local_user_login` on OSS: fixed identifiers plus a strict guard

Two properties compound. On OSS, DataHub's `NativeUserService` guard rejects signUp whenever the user entity exists *at all*, where Cloud only rejects when it already has credentials (`docs/design/local-user-login-oss-cloud-differences.md`, "NativeUserService signUp guard"). And `local-iam` hardcodes the email: `new_member_email` defaults to `tf-example-member@example.com` (`variables.tf:15`). One failed destroy therefore poisons that example permanently on any instance that survives the run.

There is a second, independent timing hazard: signUp writes aspects through `ingestProposal`, which is asynchronous relative to the HTTP response, and the provider polls with linear back-off up to ten attempts (same document, "After-signUp propagation delay"). A slow or contended runner can exhaust that budget.

Mitigations: the ephemeral Quickstart makes the first hazard self-healing, which is most of the argument for the ephemeral target. Additionally pass a per-run unique email via `TF_VAR_new_member_email` so even an accidental run against a persistent instance does not poison it -- the acceptance suite reaches the same conclusion for the same reason (`target.go:126-135`, and the "Emails must be unique per test run" requirement in the design doc). Run `local-iam` last.

### 4. `local-iam` mutates the identity the harness authenticates as

`make quickstart-token` mints the PAT for `urn:li:corpuser:datahub` (`Makefile:18`, `TOKEN_ACTOR`). `local-iam` then resolves that same user by default (`variables.tf:21`, `member_username = "datahub"`), adds it to a group (`main.tf:93-96`), and grants that group the built-in Editor role (`main.tf:109-112`).

**I did not verify what DataHub does when a user with the Admin role joins a group assigned Editor** -- whether effective privileges are the union, or whether the group assignment can narrow them. If it can narrow them, the harness's own token loses Admin partway through the run and every subsequent operation fails in a way that looks like an unrelated provider bug. Note also that the resource comment at `main.tf:107-108` states DataHub allows only one role per actor, which makes the question sharper rather than settling it.

Mitigations: run `local-iam` last, so the blast radius is bounded to itself. Add a post-example credential probe (re-read `data.datahub_me` equivalent) that reports the loss explicitly if it happens, rather than letting it surface as a cascade. Resolve the underlying question against a Quickstart before promoting this example out of the tail position -- and if the answer is bad, override `member_username` to a throwaway user instead.

### 5. Index-lagged plural data sources

Covered in detail under assertion 1. Four in-scope examples read a GraphQL `list*` plural data source whose result grows between apply and plan. Left unhandled this is a routine false failure of the highest-priority assertion. Mitigation: judge plan-cleanliness on `resource_changes` from the plan JSON, not on the `-detailed-exitcode` value alone.

### 6. `ingestion-source-lookup` depends on a bootstrap entity

The example asserts `datahub-gc` is "present on every DataHub instance" (`main.tf:16`). Whether a Quickstart that has just passed `datahub docker check` has finished creating its system ingestion sources is **unverified**. If it has not, this example fails intermittently for a reason that has nothing to do with the provider. Mitigation: preflight-poll `GET /openapi/v3/entity/datahubingestionsource/urn:li:dataHubIngestionSource:datahub-gc` with a bounded budget, skip the example with a clear message when it never appears, and record the finding here once observed.

### 7. Quickstart boot

Image pulls fail, and the first pull takes 5-10 minutes. Already handled by `quickstart-up` polling `datahub docker check` to a 600s budget (`Makefile:87-96`); stage C inherits it by reusing the target.

## Open questions

These are unresolved and should be settled empirically before or during implementation, not guessed at:

1. Does a group's role assignment narrow the effective privileges of a member who already holds a stronger role? Determines whether `local-iam` is safe anywhere but last (flakiness item 4).
2. Does a freshly healthy Quickstart always have `datahub-gc`? Determines whether `ingestion-source-lookup` is in scope unconditionally (flakiness item 6).
3. What is the real wall-clock per example? Every figure in the cost table is an estimate.
4. Is one whole-configuration destroy retry enough for `domain-simple` in practice, or does the in-provider backoff already cover it and make the harness retry dead code?

## Follow-up work this design implies

Small, independently useful, and not blocking:

- Export a `datahubtesting.AssertURNAbsent` that composes `AssertEntityAbsent`'s absence semantics with `describeStillExists`'s classification.
- Add a computed `urn` attribute to `datahub_ingestion_source`, the only entity resource missing one (`ingestion_source_resource.go:106`), which would remove the special case from the harness's URN harvest.
- `examples/runnable/provider-install-verification/main.tf` has no `required_version` constraint and declares an `output` in `main.tf`, both against the example conventions in `CLAUDE.md`. Unrelated to stage C, but it is the example the registry smoke test runs.
