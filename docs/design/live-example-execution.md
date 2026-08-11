# Live example execution (stage C)

Maintainer-facing design for running the runnable examples for real -- `terraform apply` then `terraform destroy` against a live DataHub Quickstart -- rather than only type-checking them.

**Status: the first slice is implemented.** `internal/provider/example_live_test.go` holds the harness, `internal/provider/example_live_classification_test.go` the run list and the completeness check, `.github/workflows/live-examples.yml` the CI job, and `make test-examples-live` runs it locally. Six of the twenty-one runnable examples are applied live; the other fifteen are classified as excluded, ten of them only until the expansion slice. Sections below that describe a decision still read as a design; where implementation changed one, the section says so.

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

## Scope: all 21 runnable examples classified

Every directory under `examples/runnable/` falls into exactly one primary bucket. Several carry secondary blockers that would independently disqualify them; those are named so that a later reclassification (say, if a Cloud-capable target became available) does not have to rediscover them.

**This table listed 19 directories when the design was written. Two more arrived with #116**, and both are Quickstart-capable -- see `home-page-layout` and `page-template-simple` below. The drift is worth noting rather than quietly fixing: it is exactly what `TestEveryRunnableExampleIsClassified` exists to catch, and had the harness existed at the time, #116 could not have merged without classifying them.

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
| `home-page-layout` | Runs on Quickstart | Page templates and modules are `category: core` in OSS, and OSS ships the entity model and mutations with no UI, so this is the only way to customise a home page there | **Inverts the absence assertion**: it adopts the instance's default template, so destroy restores the previous layout rather than deleting it. Also needs `TF_VAR_test_user_password` (no default, WriteOnly) and a per-run unique `test_user_email`, since `datahub_local_user_login` is subject to the OSS signUp guard |
| `page-template-simple` | Runs on Quickstart | Two modules and a template nothing points at, all OSS-capable | None -- no variables, and it changes nothing users see |
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

Totals: **16 run on Quickstart**, **3 are Cloud-only**, **2 are too expensive or slow**.

**Of the 16, the first slice runs 6**: `provider-install-verification` (preflight), `tag-simple` (index-lagged plural data source), `domain-simple` (child-delete race), `structured-and-custom-properties` (worst CAT-2583 shape), `page-template-simple` (the boring control) and `home-page-layout` (restore-on-destroy). The other ten are marked `deferred` rather than permanently excluded, so the expansion slice does not have to re-derive which is which. The slice was chosen to cover both known flake classes and the assertion inversion rather than to maximise example count -- the point of stopping at six is that every wall-clock figure below is still an estimate.

The fourth bucket, **needs-external-credentials, is empty as a primary verdict**, and that is a finding rather than an oversight. Every example that needs a real third-party credential is already excluded by a stronger reason: `remote-executor-azure` on cost, `action-pipeline-dataplex-sync` on Cloud-only, `financial-services` on cost and Cloud-only. The two Snowflake examples look like members of the bucket but are not: DataHub stores the connection blob without ever dialling Snowflake, so synthetic values apply and destroy exactly like real ones.

## Opt-in mechanism

**Recommendation: a table in Go, in a new `internal/provider/example_live_test.go`, paired with a completeness test that fails the build when a directory is in neither the run-list nor the excluded-list.**

The mechanism that matters is not the allowlist. It is the completeness test beside it. This repository has already been bitten once by a coverage set that quietly failed to grow: `examples/provider` was validated by nothing until `TestEveryExampleIsValidated` was written to reconcile the file count, and the comment at `example_validate_test.go:196-202` records that the omission "was invisible until someone reconciled the file count by hand". Stage C reintroduces exactly that hazard at directory granularity. Whatever holds the opt-in list must therefore make an unclassified example a *failure*, not an absence.

The proposed shape, mirroring `exampleExemptions` (`example_validate_test.go:46-53`):

```go
// liveExamples are the runnable examples applied and destroyed against a live
// Quickstart, in the order they run. Order is load-bearing only until the
// identifier renames and the local-iam member_username override land; after
// that the sole rule is that provider-install-verification runs first. See
// the identifier-collision section of docs/design/live-example-execution.md.
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

## Identifier collisions, and why execution is serial

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

**Serial with teardown between each** makes both collisions disappear: at most one configuration holds `urn:li:glossaryTerm:tf-example-revenue` at any instant, and the second example creates it fresh after the first has removed it. This is stronger than serial-then-teardown-at-the-end, which would still have the two configurations coexisting.

Two real collisions would be enough to force serial execution on their own. They are being removed anyway (see "Unique identifiers" below), so the case for serial has to stand without them -- and it does. Several examples read instance-wide plural data sources (item 5 under "Flakiness"); those lists return everything on the instance, so two configurations running at the same time appear in each other's outputs no matter what their entities are called. Renaming cannot decouple them. Serial is therefore the default because of that coupling and because a single-threaded run bounds the blast radius of a failed destroy, not because two ids happen to agree.

The distinction matters for anyone later tempted to parallelise: uniqueness makes it *safe to reorder*, not safe to run concurrently.

### Unique identifiers

**Decision, 2026-08-02: rename the colliding ids rather than rely on serialisation to hide them.** This design originally argued the other way -- that renaming "pays a permanent readability cost in published documentation to buy a test-harness property."

The counter-argument that carried: serialisation makes collisions harmless *while the harness behaves*, and does nothing when it does not. A destroy that fails leaves an entity behind, and the CAT-2583 husk class (item 1 under "Flakiness") is documented, reproduced, and recorded at `CHANGELOG.md:99`. An operator sweeping debris then reads a leftover `urn:li:glossaryTerm:tf-example-revenue` and cannot say which of two examples produced it. That ambiguity lives in the name, so no amount of sequencing removes it.

The readability objection is kept as a constraint on the rename rather than discarded: the replacements must make the example of origin inferable from the identifier, which is precisely the property the change exists to create. A random suffix would satisfy uniqueness and defeat the purpose. Display names get the same treatment -- they are not URN-bearing and cannot collide technically, but an operator sweeping a UI reads the display name, not the URN.

Work is in flight on `chore/unique-example-identifiers`. **Until it lands, the two collisions above are real and the ordering below must respect them.**

Proposed order, and why. Note how little of this survives its own reasoning: entries 5, 6, 11 and 12 exist only because of the collisions being removed, and 14 only because of a default that the harness can override. What is left afterwards is a single rule -- run the preflight first.

1. `provider-install-verification` -- creates nothing; fails in seconds if the PAT or GMS URL is wrong, before an hour of Quickstart time is spent. **This is the one ordering constraint that survives everything below.**
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
14. `local-iam` -- **last**, but only until the harness overrides `member_username` (see "Flakiness" item 4). The hazard is not intrinsic to the example. It exists because the example's default names `datahub`, which is also the account `make quickstart-token` authenticates as -- two independently reasonable defaults that happen to pick the same user. The table already carries a per-example `vars` map (`secret-basic` uses it for `secret_value`), so one entry retires this constraint.

Serial execution solves the collisions but does *not* by itself solve the asynchronous side effect described in "Flakiness" item 1: a structured-property delete in example 6 can land after example 7 has started. That is why the design adds an end-of-run sweep rather than relying on per-example checks alone.

## Assertions, in priority order

Four assertions per example, ordered by value per unit of maintenance. Everything below is generic -- none of it names an attribute of any resource -- which is what keeps stage C from becoming a second copy of the acceptance suite.

**Amended during implementation (2026-08-11), on three points.** The design had absence-after-destroy and no corresponding presence check, which turned out to be an asymmetry worth closing rather than a simplification:

1. **Added: every managed URN must exist after apply** (assertion 2b below). Not redundant with the plan check. A plan is clean whenever the provider's `Read` agrees with state, so a `Read` that returned prior state without consulting the server would plan clean over a server holding nothing -- a class `plan` structurally cannot see. Nearly free, because the URN harvest already exists for the destroy check: one GET per managed resource, roughly 30-40 for the whole slice.
2. **Added: re-apply after destroy** for husk-exposed configurations (assertion 5). A CAT-2583 husk carries no content aspects and is invisible in the UI; what it actually *does* is block re-creation of its own URN. A second apply tests that consequence directly instead of inferring it from an aspect probe.
3. **Amended: absence is no longer universal.** `home-page-layout` adopts the instance's default home-page template, so its destroy restores the previous layout rather than deleting it. Those addresses are listed in `mustSurvive` and the assertion inverts -- absence there would mean the restore never happened. A `mustSurvive` entry matching no harvested resource is itself a failure, so a stale expectation cannot pass by asserting nothing.

**Rejected: re-running `terraform destroy` to confirm emptiness.** After a successful destroy the state holds no resources, so a repeat run reports "No changes. No objects need to be destroyed" and exits 0 regardless of what the server still holds. Terraform's post-destroy view is definitionally empty, which is precisely why the absence check leaves Terraform and reads the entity endpoint. The whole-configuration destroy *retry* under "Cleanup guarantees" is recovery from a failed destroy, not verification of a successful one -- a different thing wearing a similar shape.

**0. Apply succeeds, destroy succeeds.** Implicit, and the single highest-value bit. A published example that does not apply is the defect stage C is chiefly hunting.

**1. `terraform plan` after apply proposes no resource changes.** This catches the whole drift and normalisation class -- a server that rewrites a value, a `Read` that maps an aspect back differently from how `Create` wrote it, a `Computed` attribute the provider forgets to set. It costs one extra plan.

Use `-detailed-exitcode` as the cheap first pass (0 means clean, 2 means changes). Do **not** treat exit 2 as the verdict. Several examples read plural data sources that are OpenSearch-backed and therefore index-lagged: `data.datahub_tags.all` (`tag-simple/main.tf:57`, backed by `searchAcrossEntities` per `tags_list.go:11-13`), `data.datahub_data_products.all` (`data-product-simple/main.tf:126`, `data_products_list.go:11-13`), `data.datahub_structured_properties.all` (`structured-property-simple/main.tf:64`), `data.datahub_ownership_types.all` (`ownership-type-simple/main.tf:45`, `listOwnershipTypes` per `ownership_types_list.go:38-44`). Between the apply and the plan the index catches up, the list grows, the output value changes, and the plan is non-empty for a reason that is not a provider defect. So on exit 2, re-run as `terraform plan -out=tfplan` plus `terraform show -json tfplan` and fail only if `resource_changes` contains an action other than `no-op`. Output-only churn is reported as a log line, not a failure.

**1b. Every managed URN exists on the server after apply.** Added during implementation. The same harvest as assertion 2, read through the same strongly-consistent endpoint, so the added cost is one GET per managed resource. `AssertURNPresent` (`internal/provider/datahubtesting/urn_presence.go`) carries the reasoning in its own failure message, because a reader hitting it will reasonably ask why the plan check did not catch it first -- and the answer is that it structurally cannot.

**2. Destroy leaves nothing** -- except addresses listed in `mustSurvive`, where the assertion inverts -- **checked through the strongly-consistent read path.** `GET /openapi/v3/entity/{lowercase-type}/{urn}` reads MySQL and is the only read the provider is permitted to trust for this (the rule and the `datahub_secret` incident that produced it are in `CLAUDE.md`). A GraphQL `list*` here would report "gone" for an entity that is merely unindexed, which is the failure mode this assertion exists to catch inverted.

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

Wall-clock, **measured 2026-08-11** on a local Quickstart (Apple Silicon, `QUICKSTART_VERSION=v1.5.0.6`). The original estimate of 1-3 minutes per example was wrong by two orders of magnitude, and the correction changes a conclusion, so the estimates are kept alongside for comparison:

| Phase | Estimated | Measured | Notes |
|---|---|---|---|
| Quickstart boot | 5-10 min | 5-10 min | Unchanged, and it dominates everything else |
| Provider build | under 1 min | ~10 s | `make install` |
| `provider-install-verification` | seconds | **0.8 s** | Creates nothing |
| `tag-simple` | 1-3 min | **0.8 s** | |
| `domain-simple` | 1-3 min (slow end) | **1.0 s**, 2.5 s with re-apply | The delete backoff did not fire at all |
| `structured-and-custom-properties` | 1-3 min | **20.7 s** | 15 s of which is the deliberate `settleAfterDestroy` pause |
| `page-template-simple` | 1-3 min | **0.8 s** | |
| `home-page-layout` | 1-3 min | **0.9 s** | Including the restore assertion |
| End-of-run sweep | under 1 min | **0.03 s** | 21 URNs |
| **Six examples, excluding boot** | 15-40 min | **~25 s** | |

`timeout-minutes: 75` stays, because the boot is what it protects against, not the examples.

Two conclusions change:

- **Expanding to all 16 Quickstart-capable examples costs almost nothing.** If the ten deferred ones behave like these six, the whole slice runs in well under two minutes. The maintenance cost of the run list, not wall-clock, is the real argument for staging the expansion.
- **The case for excluding `push` is weaker than it looked.** That argument was "booting a second Quickstart doubles the cost of every merge", and it is still true -- but the cost is entirely the boot, and none of it is the examples. If the two jobs were ever merged onto one Quickstart, running stage C on every push would add seconds. That is not proposed here, because a shared instance breaks the destroy-leaves-nothing assertion (see "Cleanup guarantees"), but the trade should be re-read with real numbers rather than the estimates that motivated it.

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

Mitigations, in preference order.

**Override `member_username` to a throwaway user via the table's `vars` map.** This removes the hazard rather than containing it, and -- the reason it is first -- it needs no answer to the union-versus-narrowing question above. That question is still open, and answering it costs a Quickstart run to settle something the harness can simply route around. The published default stays `datahub`: a runnable example cannot reference a user that may not exist on the reader's instance, and `datahub` is the only account guaranteed to be there. This is a harness concern, so it belongs in the harness.

Failing that, run `local-iam` last so the blast radius is bounded to itself, and add a post-example credential probe (re-read the `data.datahub_me` equivalent) reporting the loss explicitly rather than letting it surface as a cascade of unrelated-looking failures.

With the override in place `local-iam` has no required position, and the only ordering constraint left in the whole run is `provider-install-verification` first.

### 5. Index-lagged plural data sources

Covered in detail under assertion 1. Four in-scope examples read a GraphQL `list*` plural data source whose result grows between apply and plan. Left unhandled this is a routine false failure of the highest-priority assertion. Mitigation: judge plan-cleanliness on `resource_changes` from the plan JSON, not on the `-detailed-exitcode` value alone.

### 6. `ingestion-source-lookup` depends on a bootstrap entity

The example asserts `datahub-gc` is "present on every DataHub instance" (`main.tf:16`). Whether a Quickstart that has just passed `datahub docker check` has finished creating its system ingestion sources is **unverified**. If it has not, this example fails intermittently for a reason that has nothing to do with the provider. Mitigation: preflight-poll `GET /openapi/v3/entity/datahubingestionsource/urn:li:dataHubIngestionSource:datahub-gc` with a bounded budget, skip the example with a clear message when it never appears, and record the finding here once observed.

### 8. Deleting a structured property leaves its Elasticsearch field mapping behind

**Found by the first live run of this harness, 2026-08-11, and not previously recorded anywhere in this repository.** It is not a flake -- it is deterministic, and it makes one example un-rerunnable on any instance that survives.

`examples/runnable/structured-and-custom-properties` creates `tf-example.governance.tier` and `tf-example.governance.regions`, then destroys them. The destroy is genuinely correct: both URNs return 404 from `GET /openapi/v3/entity/structuredproperty/{urn}`, the strongly-consistent path, so the entities are gone. Applying the same configuration again nonetheless fails:

```
Structured property Elasticsearch field 'tf-example_governance_tier' collides with
existing property mapping. Qualified names that differ only by '.' vs '_' normalize
to the same field name (proposed qualifiedName='tf-example.governance.tier').
```

DataHub normalises `.` to `_` when deriving the search field name, and the mapping created by the first definition is not removed when the definition is deleted. The property therefore collides with its own residue. Note what this is *not*: there is no husk, so this is a different mechanism from CAT-2583 (item 1) and the husk classifier correctly reports nothing -- the entity really is absent.

Three consequences, in descending order of how much they matter:

- **For a user**, `terraform destroy` followed by `terraform apply` of the same structured-property configuration fails, and the only recoveries are choosing a different `property_id` or rebuilding the search index. Worth confirming against a current DataHub and filing upstream if it reproduces; the qualifiedName-collision validator itself is recent.
- **For this harness**, the ephemeral Quickstart target stops being merely preferable and becomes **mandatory** for this example. The argument under "Cleanup guarantees" was about debris accumulating; this is stronger, because a *successful* run poisons the instance for the next one.
- **For assertion 5**, the husk-exposed example cannot host the re-apply check, since it can never pass there. The flag moved to `domain-simple`, which is a legitimate host rather than a consolation: domains are the entity type the provider carries create-time husk repair for, so re-applying that configuration tests exactly what a husk does -- block re-creation of a URN whose entity is gone. Verified passing.

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
