# Terraform Provider for DataHub

Terraform Plugin Framework provider that talks to the DataHub OpenAPI v3 REST surface. Manages DataHub configuration and data objects (Ingestion Sources today; more to follow). Does not provision DataHub infrastructure.

The provider works against the open-source DataHub API and also against DataHub Cloud, since both expose the same OpenAPI surface. Some resources are Cloud-only (see "Cloud-only resources" below).

## Home and donation status

This project was donated to the open-source DataHub community and is live. v0.1.0 shipped 2026-05-23.

- GitHub repository: `github.com/datahub-project/terraform-provider-datahub`
- Terraform Registry source: `registry.terraform.io/datahub-project/datahub`
- Go module path: `github.com/datahub-project/terraform-provider-datahub`

## License model

The project is licensed primarily under **Apache-2.0** (see `LICENSE`).

A small number of files derived from the HashiCorp `terraform-provider-scaffolding-framework` template remain under **MPL-2.0** (see `LICENSE.mpl-2.0` and `NOTICE`). MPL-2.0 is file-level, sticky copyleft: modifications to those files stay MPL-2.0. The per-file `SPDX-License-Identifier` header is authoritative for each source file.

When adding files:

- **Original code** uses the Apache-2.0 header:
  ```
  // Copyright 2026 The DataHub Project Authors
  // SPDX-License-Identifier: Apache-2.0
  ```
- **Edits to existing MPL-2.0 files** stay MPL-2.0; do not strip the HashiCorp copyright notice (MPL-2.0 sections 3.1 and 3.4 require it).

## Provider scope

- **OSS API targeted.** Works against both open-source DataHub and DataHub Cloud via the OpenAPI v3 endpoints. Avoid Cloud-only proprietary endpoints unless gated and documented.
- **Configuration and data only.** This provider does not provision DataHub servers, Kubernetes clusters, databases, or other infrastructure. Use a separate Terraform stack (or a different provider) for that.

## Cloud-only resources

Some resources and data sources target DataHub Cloud exclusively and will fail with a clear error on OSS DataHub. These are documented in each resource's description. Applying against OSS is a supported no-op only when every resource in the config is OSS-compatible.

| Resource / Data Source | Reason |
|---|---|
| `datahub_remote_executor_pool` (resource + data source) | The `dataHubRemoteExecutorPool` entity type and its GraphQL mutations do not exist in OSS DataHub. The underlying mutations are also classified as `category: internal` in DataHub Cloud, meaning they carry no external API stability guarantee and may change between Cloud releases without notice. |
| `datahub_freshness_assertion` (resource) | The `upsertDatasetFreshnessAssertionMonitor` GraphQL mutation and the Monitor entity type do not exist in OSS DataHub. The mutation requires the Cloud-only monitor service layer. |
| `datahub_volume_assertion` (resource) | The `upsertDatasetVolumeAssertionMonitor` GraphQL mutation and the Monitor entity type do not exist in OSS DataHub. The mutation requires the Cloud-only monitor service layer. |
| `datahub_sql_assertion` (resource) | The `upsertDatasetSqlAssertionMonitor` GraphQL mutation and the Monitor entity type do not exist in OSS DataHub. The mutation requires the Cloud-only monitor service layer. |
| `datahub_field_assertion` (resource) | The `upsertDatasetFieldAssertionMonitor` GraphQL mutation and the Monitor entity type do not exist in OSS DataHub. The mutation requires the Cloud-only monitor service layer. |
| `datahub_schema_assertion` (resource) | The `upsertDatasetSchemaAssertionMonitor` GraphQL mutation and the Monitor entity type do not exist in OSS DataHub. The mutation requires the Cloud-only monitor service layer. |
| `datahub_action_pipeline` (resource) + `datahub_action_pipelines` (data source) | The `dataHubAction` entity type and the `upsertActionPipeline`/`deleteActionPipeline`/`listActionPipelines` mutations/queries do not exist in OSS DataHub. The feature is also experimental and Cloud-internal -- the mutations carry no external API stability guarantee and may change between Cloud releases. |
| `datahub_assertion_assignment_rule` (resource) + `datahub_assertion_assignment_rules` (data source) | The `assertionAssignmentRule` entity type and the `createAssertionAssignmentRule`/`updateAssertionAssignmentRule`/`deleteAssertionAssignmentRule`/`listAssertionAssignmentRules` mutations/queries do not exist in OSS DataHub. Rule-based auto-assignment creates freshness/volume monitors, which require the Cloud-only monitor service layer. |
| `datahub_organization_display_preferences` (resource + data source) | The `updateOrganizationDisplayPreferences` mutation, the `globalSettings` GraphQL read surface it pairs with, the `GlobalVisualSettings` PDL holding `customOrgName`/`customLogoUrl`, and the `MANAGE_ORGANIZATION_DISPLAY_PREFERENCES` privilege all exist only in the closed Cloud fork -- OSS exposes no `globalSettings` query or `updateGlobalSettings` mutation at all, so there is nothing to degrade to. The `globalSettings` entity is additionally marked `category: internal` in `entity-registry.yml`, so it carries no external API stability guarantee. Note OSS contributors cannot see the backing source. See `docs/design/provider-org-settings.md`. |

## When to mention OSS vs Cloud

**Every resource and data source schema description must begin with an availability badge.** Two constants are defined in `internal/provider/availability.go`:

- `ossAndCloudBadge` -- for resources and data sources that work on both OSS DataHub and DataHub Cloud. Renders as: **DataHub ✅ | DataHub Cloud ✅**
- `cloudOnlyBadge` -- for resources and data sources that are DataHub Cloud only. Renders as: **DataHub ❌ | DataHub Cloud ✅**

Use the constant, never inline the badge string. For Cloud-only resources, also add an entry to the table above.

**In other contexts (commit messages, PR summaries, code comments, diagnostic messages)**, default to silence on OSS vs Cloud unless the distinction is load-bearing for the reader. When in doubt, ask the user.

## Resource naming

**Rule: when a Terraform Provider resource directly represents a DataHub URN entity type, the resource name is the snake_case form of the URN type.**

DataHub URN entity types use camelCase (often with a `dataHub` prefix for DataHub-platform-specific entities). The Terraform convention is `datahub_<snake_case_type>`.

Examples:

| DataHub URN type        | Terraform resource name      |
|-------------------------|------------------------------|
| `dataHubIngestionSource`| `datahub_ingestion_source`   |
| `dataset`               | `datahub_dataset`            |
| `corpUser`              | `datahub_corp_user`          |
| `glossaryTerm`          | `datahub_glossary_term`      |

Rationale: aligning the TF surface with DataHub's URN/aspect/OpenAPI vocabulary makes docs searchable, keeps the mental model consistent between DataHub and Terraform, and avoids reinventing names. Discretion applies when no URN type directly maps to the resource (e.g. recipe document builders) - in that case prefer DataHub's UI/docs vocabulary over CLI-verb-style names.

The resource `datahub_ingestion_source` follows this rule: it maps to URN type `dataHubIngestionSource`, the OpenAPI path `/openapi/v3/entity/datahubingestionsource`, and the aspect names `dataHubIngestionSourceKey` / `dataHubIngestionSourceInfo`.

Discretion example - `datahub_service_account`: a service account has no distinct URN entity type. It is a `corpUser` distinguished by a `subTypes` aspect containing `SERVICE_ACCOUNT`, under a `service_` URN-id prefix (`urn:li:corpuser:service_<id>`). The resource is named for the DataHub UI/product concept ("Service Accounts") rather than the underlying `corpUser` type, because naming it `datahub_corp_user_*` would obscure the feature and collide with the human-user resource. It reuses the corpUser OpenAPI v3 transport but is a separate resource with its own subtype guard.

## Build and development

- Go module: `github.com/datahub-project/terraform-provider-datahub`
- Go version: 1.26.6 (pinned in `mise.toml`; `go.mod` declares `go 1.26.5` as the minimum language version and selects the compiler with `toolchain go1.26.6`)
- Tools submodule: `tools/` (holds `tfplugindocs`; its `go` directive is kept in sync with the main module)
- Build: `make install` (writes to `./bin/terraform-provider-datahub`)
- Verify: `go build ./...` and `go vet ./...`
- Generate docs: `cd tools && go generate ./...`
- Tests: `make test` (unit + mock acceptance); `make testacc` (live acceptance, requires a running DataHub instance)
- Example validation: `make test-examples` runs `terraform validate` over the examples against a freshly built provider. Needs `terraform` on `PATH`; needs no DataHub instance and no network. **Run this after editing any example** -- a snippet naming an attribute the provider does not have passes every other check, including `make generate`, and only fails for the user who copies it off the registry page. Examples split two ways in `internal/provider/example_validate_test.go`: `fragmentRoots` hold bare blocks that get wrapped before validating, and `completeExampleDirs` hold whole configurations validated in place. **A new example directory belonging to neither is a build failure** -- `TestEveryExampleIsValidated` walks every `.tf` under `examples/` and reports any file neither path reaches, which is how `examples/provider` was found to have been validated by nothing. Exemptions live in `exampleExemptions` and each carries a reason.
- Live example execution (stage C): `make test-examples-live` applies and destroys the runnable examples against a live DataHub instance, which is the only thing that catches a `Create` writing an aspect the `Read` cannot parse back, a `Delete` that silently leaves the entity in place, or a value the server normalises into a permanent diff. Against a local Quickstart use the wrappers, which mint the PAT themselves: **`make test-examples-live-quickstart`** boots, runs and tears down; **`make test-examples-live-local`** runs against one already up. `make test-examples-live` itself takes explicit `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN`, so it can also target a remote instance. `EXAMPLES="a b"` narrows any of them. **Do not reinstate the `eval "$(make quickstart-token)"` form** the error message used to suggest -- `scripts/quickstart-token.sh` prints a bare token, so eval ran it as a command, set nothing, and never set `DATAHUB_GMS_URL`; it survived because CI captures the token into a variable instead and so never exercised it. Runs nightly via `.github/workflows/live-examples.yml`, and on a PR labelled `run-live-ci`. **The run list is in `internal/provider/example_live_classification_test.go`, and every directory under `examples/runnable/` must appear in either `liveExamples` or `liveExampleExclusions`** -- `TestEveryRunnableExampleIsClassified` fails the build otherwise, and it runs under a plain `go test ./...` so a new example cannot merge unclassified. Exclusions carry a `permanent` flag separating "can never run here" (Cloud-only, or too costly) from "not in this slice yet". Read `docs/design/live-example-execution.md` before adding one: several examples need a per-example flag, and each flag traces to a documented server behaviour rather than a convenience. **Every field on `liveExample` defaults to the stronger behaviour, so an author who thinks about none of them gets the full set of checks.** In particular each example is applied a second time after its destroy and destroyed again, which is the only check that observes what a CAT-2583 husk actually does (block re-creation of a URN an aspect probe reports absent). The opt-out is `noReapplyReason`, a string rather than a bool so the skip list can be audited -- **cite the upstream issue number**, and note that the one admissible reason is "the server will not accept this configuration twice, so the check can never pass". Cost is not a reason at ~1s per example, and neither is "vacuous because the write is an upsert": that is a claim about our own code, so leaving the check on turns it into a regression guard on the claim.
- Lint: `make lint` -- **always run before raising a PR**. The linter includes `gofmt`; misaligned comment spacing (e.g., `"foo",   // comment` with wrong tab count) will fail CI even if the code compiles and tests pass.
- New worktree: after creating a git worktree for a new branch, run `make dev-override` inside the worktree directory before running any Terraform commands. This generates the gitignored `dev.tfrc` with the absolute path to that worktree's binary, and sets `TF_CLI_CONFIG_FILE` via `.mise.env`. Without it, Terraform picks up the wrong (or no) provider binary.

## Vulnerability scanning covers three Go modules, not one

`make deps-vulncheck` is `govulncheck ./...` from the repository root, so it sees the **main module only**. `tools/` and `tools/serve` are separate modules with their own `go.sum`, and they went unscanned from the day they were created -- which is how `GO-2026-5970` (`x/text` infinite loop) came to be fixed in the main module for 0.21.1 while staying reachable in both tool binaries, and how a goldmark XSS sat in the documentation generator unnoticed. **`make deps-vulncheck-tools` covers them; `make deps-vulncheck-all` runs both.** Both scans are steps in the `Vulncheck` CI job.

Three things about that target are non-obvious enough to write down:

- **It scans binaries, not source, and that is not a workaround.** A tools module's only Go file is a build-tagged stub whose blank import keeps the tool's version pinned in `go.mod` -- delete the import and `go mod tidy` drops the tool entirely. That import names a `package main`, which source-mode analysis refuses, so every source-mode invocation fails: `./...` matches no packages, `-tags generate` reports "is a program, not an importable package", and `-scan module` reports "build constraints exclude all Go files". Do not retry these. Binary mode also scans exactly what ships in the tool, since only linked code is present.
- **Reachability is the bar, matching the main-module scan.** A finding whose first trace frame names a function is called; anything else is present in the graph but not invoked, and is printed without failing. Keeping both targets to one definition of "vulnerable" is what makes their results comparable.
- **The accepted-forever list is a named constant, not a filter buried in a pipeline.** `PERMANENT_OSV` in `scripts/vulncheck-tools.sh` holds advisories with no fix in any release, each with a reason and the condition that would remove it -- today only `GO-2026-5932` (`x/crypto/openpgp`, unmaintained by design, reached by `tfupdate`). Without it the check would be permanently red, and a permanently red check is one nobody reads.

**Bumps do not propagate between modules.** Fixing a dependency in the main module leaves the tool modules untouched, and `make deps-outdated`/`deps-update` act on the main module only. Fix a tool module from inside it: `cd tools && go get golang.org/x/text@latest && go mod tidy`.

**Do not assume Dependabot will raise any of this.** Neither `GO-2026-5970` nor the goldmark XSS has a GitHub advisory entry, and `golang.org/x/mod` has none at all -- verified against the GitHub advisories API (`/advisories?ecosystem=go&affects=<module>`). Dependabot cannot raise what its database does not hold, which is the whole reason this scan exists; the two tools answer different questions and neither subsumes the other.

## How `datahub docker quickstart` resolves a version

This has confused everyone who has looked at it, twice, so here it is once. `datahub docker quickstart --version X` draws on **three separate inputs**, and conflating them is the whole difficulty:

1. **The version mapping.** `quickstart_version_mapping.yaml`, fetched from **datahub master over the network on every invocation**, which resolves `X` into a plan of `{composefile_git_ref, docker_tag, mysql_tag}`.
2. **The compose file**, downloaded from `raw.githubusercontent.com/datahub-project/datahub/<composefile_git_ref>/...`.
3. **The images**, from `docker_tag` substituted into that compose file.

**Only three-component versions pass through untouched.** The CLI's test is `^v?\d+\.\d+(\.\d+)?$`. A version matching it that is *absent* from the mapping is used verbatim as both the compose ref and the image tag -- so `v1.7.0` gets its compose from an immutable release tag, not from master. Anything else is "not recognized" and is replaced by the mapping's `default`, which uses **master** as its compose ref. This is why a four-component pin is a trap: `v1.5.0.6`, `v1.6.0.1` and `v1.4.0.3` all fail the regex. The pin read `v1.5.0.6` for months while every run booted the default, and it was found only by reading image tags off stopped containers. `quickstart-up` now rejects the four-component shape before paying for a boot.

**The mapping is upstream's hotfix channel, not a version list.** Its own comments record `v0.9.6 images contain security vulnerabilities`, `v1.4.0` remapped so `datahub-upgrade` runs `SqlSetup`, and `v1.6.0` remapped to `v1.6.0.1`. Upstream repairs a broken release for every user without anyone changing their pin -- right for someone trying DataHub for the first time, wrong for a test suite that must boot the same thing twice.

**So the mapping is pinned to a checked-in file** via `FORCE_LOCAL_QUICKSTART_MAPPING` (`scripts/quickstart-version-mapping.yaml`, exported by the Makefile). That removes a network call with a 5 second timeout and a **silent** fallback to `~/.datahub/quickstart/quickstart_version_mapping.yaml` -- a cache left by earlier runs, so an offline runner would otherwise boot from whatever some previous run happened to leave there. The accepted cost: upstream's repairs no longer arrive automatically, and taking one is a deliberate edit to that file.

**What is not lockable:** `docker_tag` is a tag, not a digest, and the CLI has no digest option. Release tags are immutable by convention only. Locking that too would mean not using `datahub docker quickstart` at all.

**CLI and server versions are separate streams that share a numbering scheme.** The CLI ships to PyPI as `acryl-datahub` (pinned in `requirements-dev.txt`); the server ships as Docker tags and GitHub releases (pinned as `QUICKSTART_VERSION`). They are not released together -- server `v1.6.0.1` appeared nine days *after* `v1.7.0` -- so any staleness check must compare versions, never dates, and "aligned" means the CLI is at or above the server line it drives, not that the numbers match. The CLI only refuses a server below `MINIMUM_SUPPORTED_VERSION` (`v1.1.0`), so a mismatched pair fails silently by working.

**The CLI pin bypasses mise's release-age guard, so bump it deliberately.** `requirements-dev.txt` is installed by `uv` straight from PyPI, which means none of the `minimum_release_age` protection described under "Tool version maintenance" applies to it -- a version published an hour ago is installable here while `mise upgrade` still withholds it. Expect a developer's global `pipx:acryl-datahub` and this pin to disagree for a day after each release, and prefer a release that has been up for more than a day rather than the newest one PyPI reports. That is the same judgement mise makes automatically for everything else.

## Tool version maintenance

Dependabot has no `mise` ecosystem support — tool versions pinned in `mise.toml` are a blind spot not covered by any automated process.

Bump pinned tools **right after cutting a release**, not before. A tool upgrade can introduce a regression, and doing it immediately before tagging leaves no buffer to catch one -- it risks derailing a release that is otherwise ready. Running it just after a release means any breakage surfaces at the start of the next development cycle, with a full cycle to shake out, and the release you just shipped is unaffected. (Also run it any time `mise.toml` has not changed in a long time.) Check and update pinned tools with:

```bash
mise outdated --local --bump   # check what is stale (--local scopes to this project only)
mise upgrade --bump            # install newer versions and rewrite pins in mise.toml
```

Always use `--local`; without it, global mise tools (e.g. `awscli`) appear as noise.

`--bump` on the check is essential: every pin in `mise.toml` is exact, and without `--bump`, `mise outdated` only verifies that the installed version satisfies the pin — an exact pin always satisfies itself, so the command reports "All tools are up to date" no matter how stale the pins are.

When bumping, hold `python` at 3.11.x (newer Pythons break `acryl-datahub` compatibility), and keep the `go` pin in sync with the `go` directive in `go.mod`, `tools/go.mod`, and `tools/serve/go.mod` (CI resolves its Go version from `go.mod` via `go-version-file`).

**`mise outdated` will not always show you a new Go release, and the gap can be a security one.** mise applies a `minimum_release_age` gate that hides recent releases -- deliberately, so a bad release is not adopted the day it lands -- and it hid Go 1.26.6 while six standard-library advisories (`net/url`, `html/template`, `crypto/tls`, `net/http`, `encoding/asn1`) sat unfixed in the shipped binary. The only thing that caught it was the `Vulncheck` CI job, which is worth knowing before anyone decides that job is noisy. `mise ls-remote --minimum-release-age 0 go` shows what is being withheld.

**The Go toolchain and the mise pin are therefore allowed to differ, deliberately, and only in that direction.** Where a stdlib advisory needs a newer compiler than mise will yet install, add a `toolchain goX.Y.Z` directive to all three `go.mod` files: Go downloads that toolchain itself (`GOTOOLCHAIN=auto`), so it works in CI regardless of mise's gate and applies to anyone who clones the repository. Do not reach for `minimum_release_age_excludes` instead -- it is tool-granular, so exempting `go` opts it out of the guard permanently rather than adopting one release early. Note the `go` directive is a minimum *language* version and stays where it is; only `toolchain` moves. Re-align once the gate lapses, by moving the mise pin up to meet the toolchain -- done for 1.26.6, so the two now agree again.

**Leave the `toolchain` directives in place after re-aligning.** They look redundant once mise offers the same version, but CI resolves its Go from `go-version-file: go.mod` and `setup-go` reads the `toolchain` line: delete those three lines while the `go` directive still names an older release and CI silently drops back to it, reinstating whatever that release's advisories are. The `Vulncheck` job would catch it, which is the system working rather than a reason to rely on it. Removing them is only safe as part of a deliberate decision to raise the `go` directive itself, which is a language-version floor for consumers and a separate call.

## Release strategy

The project is on `0.x` versioning. The `0.x` prefix is the Terraform Registry's accepted signal for "API is not stable yet"; breaking changes remain permitted until the project chooses to flip to `v1.0`.

## Resources with OSS vs Cloud behavioral differences

Some resources behave differently between OSS DataHub and DataHub Cloud. Before modifying these resources, read the corresponding design doc:

- `datahub_local_user_login` -- `docs/design/local-user-login-oss-cloud-differences.md` covers signUp endpoint path, URN derivation, invite token mechanics, propagation delay, NativeUserService guard, and import behavior. All differences were verified empirically in June 2026.

## New resource and data source design checklist

Before implementing any new resource or data source, read `docs/design/datahub-model-and-resource-design.md` in full. That document covers the reasoning behind each of these points in detail. The short checklist:

**URN strategy**
- What is the URN format for this entity type? Does the key come from the human-readable name, a user-supplied ID, or a hash?
- Does the chosen URN key match the convention used by the DataHub Python SDK (`datahub` CLI) for the same entity type? It must, to avoid duplicate-entity problems when coexisting with SDK-created entities.
- Does the entity type have non-deterministic URN creation paths (e.g., UI creates a random UUID)? Document this and ensure the provider always uses a deterministic path.
- For container-typed references: do not construct container URNs in the provider. Accept the full URN string as an input, or implement a lookup data source.

**Reference and dependency modeling**
- Does this resource reference other DataHub entities (tags, glossary terms, domains, containers)?
- Where possible, model these as Terraform expression inputs (e.g., `datahub_tag.x.urn`) rather than raw URN strings, so Terraform's dependency graph provides ordering automatically.
- Document that raw URN string inputs bypass validation.

**Upsert and list semantics**
- Does this resource manage any aspect that contains a list (tags, owners, terms)?
- If so, the resource must own the complete list and always POST the full desired state. Do not use PATCH/append semantics. Document that items added outside Terraform will be removed on the next apply.

**Delete behavior**
- Does the entity type support `status.removed` (soft delete)?
- Does the OpenAPI DELETE endpoint perform a soft or hard delete? Verify against the DataHub API.
- Are there reactivation risks if the URN is reused after deletion? Document them.

**Provider scope**
- Is this entity type platform-level configuration (owned by platform/engineering teams) or per-asset enrichment (owned by business users)?
- Resources are appropriate for platform-level configuration only. Do not implement resources for managing descriptions, tag assignments, or ownership on individual data assets - those belong to business users and will be overwritten by apply.
- Data sources are appropriate for looking up asset URNs and metadata without managing them.

**Org-level settings and singletons**
- Is this a setting on the `globalSettings` singleton (or a similar always-present platform object)? If so, read `docs/design/provider-org-settings.md` before designing it - `datahub_organization_display_preferences` is the reference implementation.
- **Group by administrative domain**, not by UI page and not by GraphQL mutation. One resource per set of settings that one persona owns, one privilege gates, and one availability tier applies to. Aspect sections are the raw material; merge where a domain spans several. Anchoring on the page breaks because pages mix org-wide and per-user settings and get rearranged; anchoring on the mutation breaks because `updateGlobalSettings` alone spans SSO, notifications, integrations and four AI groups.
- **Check the scope of every control on the page.** Every DataHub settings page examined so far mixes org-wide settings with per-user ones (`corpUserSettings`). Per-user preferences are out of scope by the provider-scope rule above; only the org-wide half is ever modelled.
- Do not collapse the settings surface into one fat `datahub_global_settings`: resources here own what they declare, so that would reset unrelated settings when a user manages one of them, and privileges, availability tiers and owning teams all differ per domain.

**Nested attributes and unknown values**
- Does the resource have a nested attribute (`SingleNestedAttribute`, `ListNestedAttribute`)? If so, read the "Unknown Values in Nested Attributes" section of `docs/design/datahub-model-and-resource-design.md` - this class of bug shipped twice, in `datahub_policy` and in two assertion resources.
- **Model nested attributes with framework types** (`types.Object`, `types.List`), never `*fooModel` or `[]fooModel`. A Go pointer or slice cannot hold an unknown value, so a block fed from a variable or `for_each` fails conversion before the provider runs.
- **Never write `!x.IsNull() && x.ValueString() != ""`** to test presence in `ValidateConfig`/`ModifyPlan`. `ValueString()` returns `""` for unknown, so a set-but-unresolved attribute reads as absent and the resource rejects a valid config. Check `IsUnknown()` explicitly and skip.
- **Add one non-literal test per resource with nested attributes.** A literal in a test config resolves schema defaults and never produces unknown, so the whole suite can pass while every module use is broken. Use `ConfigVariables` on the `TestStep`, or feed from `terraform_data.seed.output`; see `internal/provider/datahubtesting/policy_unknown.go`.
- Remember `Default` requires `Computed: true`, so giving an attribute a schema default is what makes it unknown-able. For a new attribute, consider applying the default in `Create`/`Update` and leaving it plain `Optional` instead.

**Provider-level defaults**
- Does the entity type carry `customProperties`, `globalTags`, or a structured-property aspect? If so, it is a defaults candidate - see `docs/design/provider-default-labels.md` and the "Provider-level defaults" guide.
- Add the entity's kind to the relevant mechanism(s) in `defaultsSupport` (`internal/provider/defaults.go`) and wire the resource's ModifyPlan/Create/Update/Read/Import the same way the existing default-capable resources do. Getting this wrong is silent: the resource just never picks up defaults, with no error to notice.
- Update the support matrix in `docs/guides/provider-defaults.md` and the `defaults` schema descriptions in `provider.go` to list the new resource.

## Example conventions

### Directory naming: `-simple` for the minimal example

**A runnable example that exists to show the minimal use of one resource is named `<resource>-simple`. Everything else is named for the scenario it demonstrates.**

So `tag-simple`, `domain-simple`, `structured-property-simple` are reference examples: the smallest configuration that exercises the resource, nothing decorative. `financial-services`, `local-iam`, `action-pipeline-dataplex-sync` are scenario pieces, named for what they show rather than what they use.

This was the original convention and it eroded because nobody wrote it down. Two directories drifted to `-basic` (`executor-pool-basic`, `secret-basic`) before it was noticed, and twelve of nineteen examples are scenario-named -- correctly, in most cases. **Standardise on `-simple`**; the two `-basic` names are grandfathered rather than renamed, because a directory rename changes the identifiers inside it and the uniqueness test treats those as attribution for orphaned entities.

Worth knowing the counter-argument, since it will come up: `basic` is the near-universal suffix for the minimal acceptance *test* across Terraform providers (`TestAccResourceX_basic`). It does not govern here -- it names test functions rather than example directories, providers' `examples/` trees have no shared convention at all, and this repository's own tests use `_Lifecycle`, `_List` and `_DataSource` rather than `_basic`.

A resource can have both: `page-template-simple` is the minimal reference, `home-page-layout` the demonstrative piece. When it does, keep the expensive or risky behaviour in the scenario example and let the `-simple` one stay boring -- that is what makes it useful to copy from.

### File layout

Runnable examples follow the standard HashiCorp convention for file separation:

- `main.tf` — provider block and resource/data source declarations only
- `outputs.tf` — all `output` blocks; never mix them into `main.tf`
- `variables.tf` — input variables (add when the example needs parameterisation)
- `README.md` — prerequisites, run instructions, follow-up actions, cleanup

### Terraform version constraint

Every runnable example must set `required_version = ">= 1.11"` in its `terraform {}` block. This is the minimum that supports WriteOnly attributes (`datahub_secret`, `datahub_connection`) and the `import {}` block with `for_each` -- both features users commonly pair with provider examples. Use the same constraint even in examples that don't exercise these features directly, to keep the requirement uniform across the `examples/` directory.

### Identifying Terraform-managed resources

Use a `tf-example-` prefix on machine-readable IDs (e.g. `group_id`, `policy_id`, `source_id`, `connection_id`) and a `TF Example ` prefix or similar marker on human-readable display name strings (e.g. `name`). This convention -- already established by `TF_EXAMPLE_SECRET_BASIC`, `"TF Example Tag - PII"` etc. in other examples -- lets operators immediately distinguish resources created by the runnable example from those created via the DataHub UI, CLI, or SDK. It also makes cleanup straightforward: any resource with a `tf-example-` id or `TF Example` name in its display was created by an example.

### Every identifier is unique to one runnable example

**No two directories under `examples/runnable/` may create the same DataHub entity, use the same id string, or show the same display name.** The reason is orphan attribution: examples get applied against shared and disposable instances, cleanup sometimes fails (the CAT-2583 husk survives `terraform destroy`), and an operator sweeping the debris has to be able to look at a leftover URN or UI label and know which example produced it. When two examples can produce the same one, that inference is impossible.

The scheme that delivers it: **`tf-example-<example-slug>-<name>`**, where `<example-slug>` is a short fixed token naming the origin directory, and the matching display name is `TF Example <Slug> - <Name>`. So `examples/runnable/domain-simple` owns `tf-example-domain-finance` / "TF Example Domain - Finance", and `examples/runnable/glossary-node-term-simple` owns `tf-example-glossary-finance` / "TF Example Glossary - Finance". Adding the slug is what makes the identifier self-describing; without it, "unique" would be satisfied by a random suffix that tells a sweeper nothing.

Current slugs, one per directory: `dataplex`, `sqlite`, `snowflake` / `snowflake-ingest` (the two connection examples), `dp`, `domain`, `pool`, `fibo`, `glossary`, `csv`, `iam`, `ownership`, `azure`, `secret`, `governance`, `property`, `tag`, `homepage`, `pagetpl`. A directory whose name already reads as the slug (`assertion-volume-sqlite` -> `tf-example-sqlite-assertion`) needs nothing more. DataHub secret names keep their SCREAMING_SNAKE convention and take the same shape uppercased: `TF_EXAMPLE_SECRET_BASIC`, `TF_EXAMPLE_AZURE_ABS_ACCOUNT_NAME`.

Two further points, both learned the hard way:

- **Compare resolved URNs, not id strings.** A URN is namespaced by entity type, so a domain and a glossary node sharing an id do not collide. Conversely, do not assume every identifier carries the `tf-example-` prefix: the `urn:li:dataHubConnection:prod-snowflake` collision between the two Snowflake examples went unnoticed for exactly that reason.
- **Give every `datahub_ingestion_source` an explicit `source_id`.** It is optional, and when omitted DataHub derives `<sanitized-source_name>-<hash>`, which is neither predictable nor greppable. Setting it keeps the URN deterministic and traceable.

This is enforced, not merely written down. `internal/provider/example_identifier_test.go` parses every `.tf` under `examples/runnable`, resolves each managed resource to its URN (following variable defaults and locals), and fails on any URN, id string or display name claimed by two directories. It runs under a plain `go test ./...` -- it needs neither terraform nor a provider binary. A resource type used by an example and classified by neither `urnKeyedResources` nor `urnlessResources` is also a failure, so a new resource type cannot silently escape the check.

The snippets under `examples/resources/` and `examples/data-sources/` are exempt from uniqueness among themselves -- nothing applies them, and several deliberately share a `data-platform` group or a `finance` domain so the registry pages read as one estate. They may not, however, claim a URN a runnable example creates; `TestRegistrySnippetsDoNotClaimRunnableURNs` enforces that, and the fix is always to rename the runnable example, since the published snippet is what users read.

### Ingestion source types in examples

When an example includes a `datahub_ingestion_source` to illustrate a point (e.g. wiring up an executor pool), choose the source `type` to make the surrounding story self-evident:

- Prefer private-network types (`postgres`, `mysql`, `mssql`) when demonstrating VPC or executor pool patterns. A database behind a firewall is immediately understood as something that needs a private executor -- the connection story requires no explanation.
- Avoid cloud-warehouse types (`bigquery`, `snowflake`, `redshift`) in executor pool examples: these services are reachable from the internet and do not need private VPC access, which undercuts the narrative.
- `csv-enricher` and `demo-data` are fine for fully generic demonstrations where the source type is irrelevant to the point being made.

### Outputs

Always include outputs that let the user verify or act on the result of their `terraform apply` without leaving the terminal. At minimum:

- Expose any IDs or URNs that identify the created resource (e.g. `source_id`, `source_urn`).
- Where a follow-up action is natural and involves dynamic content (IDs, URNs), emit the complete command as an output value — use HCL interpolation and `jsonencode` to bake the computed values in. The user can then copy the command directly from the apply output or run it via `eval "$(terraform output -raw <name>)"`.
- Where the follow-up command cannot be fully pre-built (e.g. it depends on a value returned by a previous step), put it in the README referencing `$(terraform output -raw <name>)` for the dynamic parts.
- If the DataHub UI is the most natural place to verify the result, include the navigation path and a direct URL template (e.g. `$DATAHUB_GMS_URL/ingestion` for ingestion sources).

The goal: a user who has just applied the example can verify the result and take the logical next step without hunting through docs.

## DataHub API: eventual-consistency trap

DataHub exposes two read paths with very different consistency guarantees:

- **GraphQL `list*` queries** (e.g. `listSecrets`, `listIngestionSources`): backed by OpenSearch/Elasticsearch. Eventual-consistency -- a resource created seconds ago may not yet appear. Never use these for Terraform Read or ImportState operations.
- **OpenAPI v3 entity endpoint** (`GET /openapi/v3/entity/{type}/{urn}`): reads directly from MySQL (the primary datastore). Strongly consistent. Always use this for Read and ImportState.

The wrong choice caused `datahub_secret` to show a spurious "plan to delete" immediately after creation: `listSecrets` returned empty because OpenSearch had not yet indexed the new resource. Fixed in PR #7 by switching `GetSecretByURN` and `ImportState` to the OpenAPI v3 path.

**Rule for every new resource:**
- Read / ImportState: `GET /openapi/v3/entity/{type}/{urn}` (MySQL, consistent)
- Create / Update / Delete: GraphQL mutations -- OpenAPI write endpoints bypass service-layer business logic (e.g. SecretService encryption)
- Search / list for non-managed lookup (data sources, imports by name): GraphQL `list*` is acceptable but document the lag risk

The OpenAPI v3 entity endpoint for a type is always `/openapi/v3/entity/{lowercase-urn-type}/{urn}`, e.g. `/openapi/v3/entity/datahubsecret/{urn}`.

## CHANGELOG.md editing conventions

The `before.hooks` in `.goreleaser.yml` extract the current version's section from `CHANGELOG.md` at release time using an awk script. The script has two constraints that are non-obvious at edit time:

1. **Use inline links inside version sections.** The awk stops when it hits a line starting with bare `[` (the reference-definition block at the bottom of the file). A reference-style link definition inside a section body — e.g. `[my-link]: https://...` on its own line — would prematurely terminate the extract. Always use inline form: `[label](url)`.
2. **Keep the `## [X.Y.Z]` heading format.** The awk matches on `^## \[X.Y.Z\]`; the version number must be in square brackets immediately after `## `.

The reference-link definitions block at the very bottom of the file (`[X.Y.Z]: https://...`) must stay in that position and continue to use bare `[` lines — this is what the awk uses as the extract stop signal.

If a `## [X.Y.Z]` section is missing for the version being tagged, GoReleaser fails before building any artifacts (the hook asserts `.release-notes.md` is non-empty). Fix: add the CHANGELOG entry, then re-tag.

## DataHub domain vocabulary (quick reference)

- **Ingestion Source** - the configured, persisted entity in DataHub that represents one source-of-metadata. Resource-shaped.
- **Recipe** - the YAML/JSON configuration document that *defines* an ingestion source's connector, options, and sinks.
- **ingest** - the verb. Either ad-hoc CLI execution (`datahub ingest -c recipe.yaml`) or the act of running a deployed Ingestion Source.

Keep this distinction in code, docs, and resource naming.
