# Changelog

All notable changes to this provider will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **New resource `datahub_glossary_term_relationship`** manages a single typed relationship edge between two glossary terms, backed by the `addRelatedTerms`/`removeRelatedTerms` GraphQL mutations: `term_urn` inherits from (`isA`, "Inherits" in the UI) or contains (`hasA`, "Contains" in the UI) `related_term_urn`. One resource per edge, following `datahub_corp_group_member`, so relationships added outside Terraform on the same terms are left untouched -- the alternative, a list on `datahub_glossary_term`, would have made Terraform silently delete every edge a steward added through the UI. The edge is stored one-sided on the source term's `glossaryRelatedTerms` aspect (DataHub writes no inverse edge; the UI's "Inherited by"/"Contained by" views are reverse graph lookups), which is what makes exactly one Terraform resource the owner of each edge. Import by composite ID: `<term_urn>|<relationship_type>|<related_term_urn>`.

  Two server behaviours shaped the implementation. The mutations perform a non-atomic read-modify-write of the aspect, so the provider serializes writes per source term -- without that, two edges on one term applied at Terraform's default parallelism could silently lose one (the CAT-2568 shape). And `removeRelatedTerms` errors when the aspect or list is already gone, so delete reads first and treats an absent edge as success, letting `terraform destroy` survive a term removed out-of-band. The aspect's other two lists (`values`, `relatedTerms`) have no GraphQL write path and are not exposed.
- **New resource `datahub_metadata_test` plus `datahub_metadata_tests` and `datahub_metadata_test_validation` data sources**, bringing DataHub metadata tests -- declarative governance rules such as "every PROD dataset must have an owner" -- under Terraform management. The `test` entity and its create/update/delete API exist on both OSS DataHub and DataHub Cloud, but only Cloud ships the engine that evaluates the rules and the UI that manages them, which makes Terraform the natural authoring surface: the API is the only management surface OSS has, and on Cloud the definition becomes reviewable, diffable configuration instead of a UI-only artifact. The resource always supplies an explicit URN id (deriving one from the name when `test_id` is omitted), because the server otherwise mints a random UUID and the test could never be matched across environments; UUID-suffixed tests already created in the Cloud UI are adopted by enumerating `datahub_metadata_tests` into `import {}` blocks.

  The definition travels as a JSON document compared by semantic equality, so formatting and key-order changes never produce a plan diff. On DataHub Cloud the server validates the definition inside `createTest`/`updateTest` and the apply fails with the engine's messages; the Cloud-only `datahub_metadata_test_validation` data source moves that same check to `terraform plan` for those who want it earlier, as an explicit opt-in because it puts a network call inside plan. It is deliberately not built into the resource's plan phase: OSS has no `validateTest` at all, an automatic call would make every plan depend on connectivity, and Cloud repeats the identical validation at apply anyway.

  Two behaviours worth knowing before adopting it: `updateTest` rebuilds the whole `testInfo` aspect from its input, so a run schedule or status configured in the Cloud UI is reset by the next apply -- the resource owns the aspect in full; and destroy is a hard delete after which results already recorded on other entities keep referencing the deleted URN until the next test run recomputes them.

## [0.22.0] - 2026-08-15

### Added

- **`datahub_structured_property` now takes an optional `version`**, which is the only durable way out of a trap the previous entry documented but could not escape. Once any asset carries a value for a property, deleting the property does not release the Elasticsearch field name DataHub derived from its qualified name, so creating the same `property_id` again is refused with "Elasticsearch field ... collides with existing property mapping". Before this, a Terraform user facing that had two options: ask an operator to reindex every entity index, or burn a fresh `property_id` each cycle -- which buys one more turn rather than fixing anything, since assigning the new name burns that too. A versioned definition derives a different field name, so it is not the same field and cannot collide. Upstream recommends exactly this in [datahub-project/datahub#18974](https://github.com/datahub-project/datahub/issues/18974), as the alternative to hard-deleting a property you need to change. Changing the value updates the property in place on the same URN rather than replacing it, and the `datahub_structured_property` data source now reports the current version so you know what you have to exceed.

  Do not reuse a version, though. A versioned field is burned by an assignment exactly as the un-versioned one is, so re-creating a deleted property at a version it has already carried collides all over again -- measured, not reasoned about: an apply at an earlier version fails with the same message, while a fresh one succeeds. Take a new timestamp each cycle. What `version` buys is an unlimited supply of names for one `property_id`, not immunity.

  The accepted format is narrower than DataHub's own field documentation claims: exactly 14 digits, a `yyyyMMddHHmmss` timestamp. The upstream field comment suggests `v1` and `20240610` and the server rejects both, verified against Quickstart v1.7.0, where each returns an HTTP 400 whose body is a validation-exception dump of the whole aspect rather than anything that names the real constraint. The provider therefore rejects a malformed value at plan time, which also subsumes the separate no-dots rule the server applies when a version is used to permit a breaking change.

  One thing it does not do yet: make a breaking change *in place*. DataHub will accept a narrowed cardinality, a changed `value_type` or a removed allowed value when the version increases, but the provider still plans a destroy-and-create for those, which hard-deletes the property and takes every assigned value with it. Bumping `version` in the same apply does keep the re-create from colliding, so the change lands -- the assigned values just do not survive it.

### Fixed

- **Running the live example suite locally now takes one command, and the instruction it used to print did not work.** `make test-examples-live` demanded `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN`, and told anyone who hit that to run `eval "$(make quickstart-token)"` -- but `scripts/quickstart-token.sh` prints a bare token rather than shell `export` lines, so that command ran the token as a command name, set nothing, and never set the URL at all. Following the advice produced the same error a second time with no indication why. It survived because CI captures the token into a variable and passes it inline, so the documented path was never the executed one.

  `make test-examples-live-quickstart` now boots a Quickstart, runs the examples and tears down on exit; `make test-examples-live-local` runs against one already up. Both mint the token themselves, so there is nothing to export. These mirror the long-standing `testacc-quickstart` and `testacc-local` exactly -- the older live targets always had these conveniences, and the live example suite simply landed as its own slice without them. `make test-examples-live` keeps taking explicit values so it can still target a remote instance, and its error message now names all three options.

### Security

- **The two tool modules were never being scanned for vulnerabilities, and had accumulated three reachable findings.** Nothing in this affects the published provider binary: `tools/` and `tools/serve` pin the documentation generator and the example version bumper, which run at development time on our own inputs. What matters is why nobody knew. `make deps-vulncheck` is `govulncheck ./...` from the repository root, so it covers the main module and stops there, and this repository has three Go modules -- a fact no scan, alert or update cadence was accounting for. The clearest evidence of the gap: `GO-2026-5970`, an infinite loop in `x/text`, was found and fixed in the main module for 0.21.1 and remained reachable in *both* tool binaries the whole time, because the scan that found it could not see them and nothing carried the bump across. Alongside it, `GO-2026-5320`, a cross-site-scripting flaw in goldmark, reachable in the documentation generator that renders our Markdown.

  Neither advisory has a GitHub entry, so waiting was not a strategy: Dependabot cannot raise what its database does not hold, and both live only in the Go vulnerability database. `x/text`, goldmark, `x/mod` and `x/net` are updated in both modules, which clears everything with a fix available. `GO-2026-5932` (`x/crypto/openpgp`, unmaintained and unsafe by design) remains reachable from the version bumper and has no fix in any release, so it is accepted explicitly rather than silently.

  A new `make deps-vulncheck-tools` scans both modules in CI so the next skew is caught rather than discovered. It builds each pinned command and scans the binary, because a build-tagged tools stub cannot be scanned from source at all -- the blank import that keeps the tool's version pinned is a `package main`, which source-mode analysis refuses. Binary mode also scans exactly what runs, since only linked code is present. Findings the tools do not call are printed and do not fail the run, matching what the main-module scan already means by "vulnerable", and the accepted-forever list is a named constant with a reason and a removal condition per entry -- an unfiltered scan would be permanently red and would stop being read.

## [0.21.1] - 2026-08-14

### Security

- **The provider is now built with Go 1.26.6, which fixes six standard-library advisories reachable from this code**: `net/url` (GO-2026-6218), `html/template` (GO-2026-6091), `crypto/tls` (GO-2026-6090), `net/http` (GO-2026-6089), `encoding/asn1` (GO-2026-5972) and one further finding. A Terraform provider spends its life making HTTPS calls and parsing what comes back, so `crypto/tls` and `net/http` are squarely on the path rather than incidental. Nothing in the provider's own code changed; a released binary built from 1.26.5 carries the vulnerable standard library, and one built from 1.26.6 does not.

  **Affected versions: 0.21.0 and every earlier release**, all of which were built with Go 1.26.5 or older and therefore ship the vulnerable standard library. Releases built with older Go carry these findings and potentially others. **There is nothing to rotate or reconfigure -- upgrading the provider is the whole fix**, unlike the credential-logging issue in 0.19.1. If you pin the provider version, this is the release to move to.

  Selected via a `toolchain go1.26.6` directive rather than by moving the pinned toolchain, because the version manager used for development withholds very recent releases by design and had not yet offered 1.26.6. Go downloads the named toolchain itself, so this applies to CI and to anyone who clones the repository. The `go` directive is unchanged at 1.26.5: it declares the minimum language version, not the compiler, and nothing here needs newer language features.

### Changed

- The runnable examples are now applied and destroyed against a live DataHub instance nightly, rather than only type-checked. `terraform validate` never contacts a server, so it cannot see a `Create` that writes an aspect the `Read` cannot parse back, a `Delete` that silently leaves the entity in place, or a value the server normalises into a permanent diff -- and the acceptance suite, which does catch those, works from its own configurations and so says nothing about the ones published here. Six examples run in the first pass; the remaining fifteen are individually classified, and a new example belonging to neither list fails the build. Run it locally with `make test-examples-live` against a Quickstart.

  One finding came out of the first live run that is worth knowing if you manage structured properties. **Once a structured property has been assigned to an entity, deleting the property does not release its Elasticsearch field name.** The entity is genuinely gone, but creating the same `property_id` again is rejected with "Elasticsearch field ... collides with existing property mapping". So `terraform destroy` followed by `terraform apply` of the same configuration fails, once any value was assigned.

  Assigning is the trigger, not defining, and not punctuation in the name: verified by experiment, a property that is defined but never assigned re-applies cleanly whether or not its `qualifiedName` contains dots, and one that was assigned fails even with a dot-free name. Disregard the part of DataHub's error message about `.` versus `_` -- that describes two *different* names colliding on one field, and the same wording is reused here. This is also distinct from the resurrection-husk behaviour the provider already works around: there is no husk, and the provider's delete is correct.

  [datahub-project/datahub#18974](https://github.com/datahub-project/datahub/issues/18974) records the behaviour as intended, with mapping reclaim deferred to the system-update job. Renaming the `property_id` buys one more cycle rather than fixing it, since assigning the new name burns that too; the durable answer is to bump the property's `version` instead of destroying and recreating. Consequently `examples/runnable/structured-and-custom-properties` cannot be applied twice against one instance, which its README documents rather than works around.

  Two examples are affected by that, not one. `examples/runnable/data-product-simple` assigns its marker structured property through the provider's `defaults.structured_properties` rather than an explicit assignment resource, and it burns the field name just the same -- confirmed by measurement, not inferred from the shape of the configuration. The rejection lands on the *definition* the next time it is created, which is the mechanism stated exactly: assignment writes the Elasticsearch field, and the following attempt to define the same qualified name is what fails.

- The live example run now applies each example a second time after destroying it, then destroys it again and asserts that too, rather than doing so for one example out of six. This is the only check that observes what a resurrection husk actually does: the entity is genuinely absent from the strongly-consistent read path, so an aspect probe reports success while the next `terraform apply` is refused with "already exists". Measured at roughly 0.6 seconds per newly-covered example -- 27 seconds against 25 for the whole local run, next to a 178-second instance boot -- which is what made the previous opt-in allocation indefensible.

  The second destroy earns its own assertions rather than being handed to the test's cleanup: it is the only teardown that follows a re-creation, so it is the only one that can show a delete path working on a freshly created entity and not on a re-created one. URNs are re-harvested before it and required to match the first harvest, because a re-created entity landing on a different URN would leave debris that nothing checks while the absence assertion dutifully confirms the old URN is gone -- and because deterministic URN derivation across re-creation is what every `import` block and every second apply depends on, and nothing else in the suite exercises it.

  Examples that cannot pass the check opt out with a documented reason citing the upstream issue, rather than a boolean flag: a flag records that somebody decided and not why, which makes a skip installed for a confirmed server behaviour indistinguishable from one installed because CI went red. Only one reason is admissible -- the server will not accept the same configuration twice -- and in particular "this write is an upsert so re-applying proves nothing" is not, since that is a claim about the provider's own code and leaving the check on turns it into a regression guard on the claim.

## [0.21.0] - 2026-08-09

### Added

- **Home-page layout is now managed in Terraform**, via `datahub_page_module` for each widget, `datahub_page_template` for the rows they sit in, and a read-only `datahub_home_page_settings` data source. Both resources always send a deterministic URN derived from the id you supply, which is what DataHub's own UI does not do -- create a template through the UI and DataHub mints a random UUID, so an estate that is torn down and rebuilt comes back with a different template every time. Fixed ids mean a demo or test instance rebuilds with its landing page intact. Module `type` is passed to DataHub unvalidated rather than checked against a list compiled into the provider: DataHub's module catalogue grew from 22 types to 30 in a single Cloud release, and a compiled-in list would make each new type unusable until the provider shipped a release.
- Changing the page users actually see means editing the template DataHub already points at, not creating a new one. DataHub seeds one default template per instance and offers no API to aim the pointer elsewhere, so read the id from `datahub_home_page_settings` and manage that template -- `data.datahub_home_page_settings.current.default_template_id`. A new `GLOBAL` template under an id of your own applies successfully and is then never rendered to anyone, which is a quiet way to waste an afternoon. `terraform destroy` on the template the instance points at is **refused**, because it would leave the organisation with no home page and a dangling pointer; use `terraform state rm` to stop managing the layout instead. That check is best-effort by design: if the pointer cannot be read at all the delete proceeds with a warning, since a guard that makes a resource permanently undeletable whenever an unrelated read fails is worse than the hazard it prevents.
- Two runnable examples. `examples/runnable/page-template-simple` is the minimal one: two modules and a template that nothing points at, so it changes nothing users see. `examples/runnable/home-page-layout` replaces the organisation's home page, publishes a second template users can opt into, and can create a test user to show that the default reaches people other than the account that applied it.

## [0.20.0] - 2026-08-02

### Changed

- Every identifier in the runnable examples is now unique to exactly one example, so an entity left behind on a test instance names the example that created it. Two examples previously created the same DataHub entity -- `glossary-node-term-simple` and `structured-and-custom-properties` both claimed `urn:li:glossaryTerm:tf-example-revenue`, and the two Snowflake examples both defaulted `connection_id` to `prod-snowflake` -- so applying both concurrently failed with "already exists", applying them in sequence left two Terraform states each believing they owned one entity, and an orphan bearing either URN could have come from either directory. That last case is not hypothetical: cleanup does fail, the structured-property resurrection this provider works around survives a `terraform destroy`, and the operator sweeping the debris had no way to attribute it. Three more id strings and four display names were shared across directories without colliding technically, each one rename of an entity type away from becoming a real collision. Identifiers now carry a slug naming their origin (`tf-example-domain-finance` versus `tf-example-glossary-finance`, "TF Example Domain - Finance" versus "TF Example Glossary - Finance"), which buys the attribution the bare names could not. Every `datahub_ingestion_source` in the examples also sets `source_id` explicitly rather than letting DataHub derive `<sanitized-name>-<hash>`, since a derived id is neither predictable nor greppable. **If you applied a runnable example before this release and then pull the new text, Terraform plans a replacement** -- the id is the URN suffix, so changing it forces a new resource. Destroy with the old configuration first if you want the old entities removed rather than orphaned. The published registry snippets under `examples/resources/` and `examples/data-sources/` are unchanged.

- A new structural test enforces the above rather than leaving it to review. `internal/provider/example_identifier_test.go` resolves every managed resource under `examples/runnable` to the URN it produces -- following variable defaults and locals, because the `prod-snowflake` collision lived in a variable default and would have been invisible to a scan that read only literals -- and fails the build on any URN, id string or display name claimed by two directories, on any snippet that claims a runnable example's URN, and on any resource type it does not know how to resolve. That last check is the one that keeps the others honest: without it a newly-added resource type contributes no identifiers, every assertion keeps passing over the types it already knew, and the new one is checked by nothing. The tests need neither `terraform` nor a built provider, so they run under a plain `go test ./...`.

### Fixed

- **`datahub_policy` no longer destroys a policy's role-based actors.** A DataHub policy can grant its privileges to DataHub roles (`Admin`, `Editor`, `Reader`) as well as to users and groups, but DataHub's write API cannot express that: the `updatePolicy` mutation has no field for roles, and the server rebuilds the entire policy from the mutation's input. Every write therefore deleted `actors.roles`. Nine of the sixteen policies DataHub ships bind their actors through roles alone -- no users, no groups, not "all users" -- so a single apply against one of them left it granting nothing to anybody, silently and with no error. Repair needed a direct aspect write, because saving the policy in the DataHub UI goes through the same mutation and would strip it again; if the gutted policy was the one carrying `MANAGE_POLICIES`, there might have been no principal left able to do it. The provider now reads the policy immediately before every create and update, refuses the write when roles would be lost, and names them in the diagnostic. This is a server-side limitation, tracked upstream as OSS-1216 -- the provider can only decline to trigger it, so no provider version makes such a write safe.

  Reaching this took no unusual configuration. The `datahub_policies` data source documented feeding its `urns` straight into an `import {}` `for_each`, and both it and `datahub-tf-extract enumerate` return DataHub's own policies alongside yours; the resource also sends `description = ""` when the attribute is unset, which by itself produces a diff against a default policy's non-empty description. Bulk-import an estate, apply once without changing anything, and the role bindings were gone. `terraform import` of a role-bearing policy now emits a warning at import time, which on that path lands during `plan` -- before any apply can run.

- `datahub_policy` warns when asked to manage a policy DataHub marks `editable = false`. Nothing is destroyed, so this is a warning rather than a refusal, but two things follow that are worth knowing before the first apply: `PolicyUpdateInput` carries no `editable` field, so every apply resets the flag to `true` and the policy becomes editable in the DataHub UI; and DataHub re-ingests its non-editable default policies from its bootstrap file on every deployment, so Terraform and the server overwrite each other and every DataHub upgrade is followed by a plan showing the same diff.

- Provider-level `defaults.structured_properties`: a default the referenced property definition cannot accept no longer leaves an orphaned entity behind. The check ran *after* the resource had already been written to DataHub, so `Create` wrote the entity, failed on the property, and returned an error without setting state - leaving Terraform with no state entry, `destroy` with nothing to remove, and the entity stranded on the server where neither plan nor state could see it. The next apply with a corrected default then failed with "already exists". The case a configuration hits in practice is a value the property's value type rejects - `"gold"` for a `number` property, say, which the provider itself refuses before the request is even sent - and a definition deleted between plan and apply fails the same way. Affected every resource that supports `defaults.structured_properties`: `datahub_domain`, `datahub_glossary_node`, `datahub_glossary_term`, `datahub_corp_user`, `datahub_corp_group`, `datahub_service_account`, `datahub_data_product` and `datahub_data_contract`. The definition lookup and the value check now both run before any write, so a bad default fails with no side effects. `Update` is unchanged and did not need this: the entity is already in state there, so a failed update is an ordinary retryable error rather than an orphan. One residue remains by nature: a value only DataHub can reject - an `allowed_values` violation, or several values on a `SINGLE`-cardinality property - is still refused after the entity exists, because the provider does not replicate the server's validator.

## [0.19.1] - 2026-08-01

### Security

- **The provider no longer writes credentials to the Terraform debug log.** With `TF_LOG=DEBUG` (or `TRACE`) set, `datahub_local_user_login` logged the sign-up request body and the outgoing request headers verbatim, which meant the new user's password, the org invite token, and the `gms_token` bearer credential were all written to the log in clear text. `datahub_ingestion_source` separately logged the full upsert response body, and because the OpenAPI v3 entity endpoint echoes the stored aspects back, a recipe holding a literal credential rather than a `${SECRET}` reference was written there too. All three call sites now log a length instead of the value.

  **Affected versions: 0.4.0 through 0.19.0** for the sign-up leak (the resource has behaved this way since it shipped), and all versions for the ingestion-source response. Nothing leaks unless debug logging is explicitly enabled, and nothing is written to state, to plan output, or to the network -- but Terraform debug logs are routinely attached to bug reports, pasted into chat, and retained as CI artifacts. **If you have ever run this provider with `TF_LOG=DEBUG` and shared the resulting log, treat the `gms_token` it contains as disclosed and rotate it**, along with any password or recipe credential that appeared in the same run.

  Note that `initial_password` is declared both `Sensitive` and `WriteOnly`, and those markers were working correctly: they govern state serialization, plan rendering, and diagnostics. They do not reach `tflog`, which writes exactly the fields the provider hands it. That gap is why the leak survived sixteen releases, and a new structural test (`TestNoSecretsInLogFields`) now fails the build if any log call carries a credential-bearing field key.

## [0.19.0] - 2026-08-01

This release changes no provider behaviour. Every resource, data source and schema is identical to 0.18.0; what changes is the documentation the registry serves and the tests that keep it honest.

### Added

- Example Usage sections for 22 registry pages that had none: the `datahub_action_pipeline`, `datahub_custom_assertion`, `datahub_data_product`, `datahub_field_assertion`, `datahub_freshness_assertion`, `datahub_ownership_type`, `datahub_schema_assertion`, `datahub_sql_assertion`, `datahub_structured_property`, `datahub_tag` and `datahub_volume_assertion` resources, and the `datahub_action_pipelines`, `datahub_assertion`, `datahub_assertions`, `datahub_data_product`, `datahub_data_products`, `datahub_ownership_type`, `datahub_ownership_types`, `datahub_structured_properties`, `datahub_structured_property`, `datahub_tag` and `datahub_tags` data sources. The documentation generator omits the heading entirely when no snippet exists rather than rendering an empty one, so nothing on the page indicated anything was missing -- a reader landing on the `datahub_freshness_assertion` page found a complete attribute reference and no illustration of how the attributes fit together. The typed assertions are the pages this hurt most, since their nested schedule and threshold blocks are difficult to assemble from the reference alone.
- The `datahub_organization_display_preferences` data source has its own example instead of sharing the resource's, which showed a write where the reader wanted a read.

### Changed

- `make test-examples` runs `terraform validate` over every example in the repository against a freshly built provider, and CI now runs it on every pull request. Examples were previously verified by nothing: `make generate` renders a snippet into its registry page without asking the provider whether the snippet is valid, so an attribute that had been renamed or removed produced clean, committed documentation and a green build, and failed only for the person who copied it. A new kind of example directory reached by neither the snippet walk nor the whole-configuration walk now fails the build rather than silently validating nothing. The check needs no DataHub instance and no network, and takes about two seconds.
- The nightly registry smoke test now launches the released binary rather than only downloading it. `terraform init` verifies a provider's checksum and unpacks it but never starts the process, so a build for the wrong architecture, or one that crashed on startup, passed the test that existed to catch exactly that.

### Fixed

- The `assertion-volume-sqlite` runnable example is now valid Terraform. Its `datahub_ingestion_source` block used `name`, `type` and a `schedule` block, none of which the resource has ever accepted -- the attributes are `source_name`, a computed `source_type`, and `cron_interval`/`timezone`. `terraform validate` rejected the example outright, so anyone following it hit four errors before reaching a plan. Broken since the example was added in 0.9.0 and shipped that way in every release since.

## [0.18.0] - 2026-07-31

### Added

- The provider now warns when the resolved GMS endpoint uses plaintext `http://` and the host is not loopback. The GMS token is sent as an `Authorization` bearer credential on every request, so over `http` it travels in cleartext and is readable by anything on the network path. A warning rather than an error, because `http` to a private-network or tunnelled endpoint is legitimate; `localhost`, `127.0.0.0/8` and `::1` are exempt, since plaintext loopback is the documented DataHub Quickstart setup.

### Removed

- **BREAKING: the provider no longer reads credentials from the DataHub CLI's `~/.datahubenv`.** `gms_url` and `gms_token` now resolve from the provider configuration, then from `DATAHUB_GMS_URL`/`DATAHUB_GMS_TOKEN`, and nowhere else. If a configuration supplied neither, the provider previously fell back to `gms.server` and `gms.token` in `~/.datahubenv` -- so `provider "datahub" {}` with an empty body was not inert: it authenticated against whichever instance that machine's CLI had last been pointed at, which could be production, with nothing in the Terraform configuration to say so. Two engineers running the same configuration against the same state could target different instances, which defeats the reproducibility Terraform exists to provide. It is appropriate for the CLI, where a human drives one command at a time, and a category error for a declarative tool.

- The `gopkg.in/yaml.v3` dependency, which existed only to parse `~/.datahubenv`. One fewer transitive dependency for anyone vendoring or auditing the provider's supply chain.

### Fixed

- `datahub_policy`: combining `resources.filter` with a legacy `resources.type`, `resources.resources` or `resources.all_resources` is now rejected even when the legacy value comes from a variable, a module output or another resource's attribute. Previously the conflict was reported only for literal values, so the same module could error for one caller and stay silent for another depending on how the caller supplied it. DataHub ignores the legacy attributes when a filter is set, so a configuration that slipped through produced a policy scoped more broadly than it read -- for example a scope written as "datasets carrying this tag" that in fact granted the privilege on every entity type carrying it. Setting only the legacy form, computed or not, remains valid and is unaffected.

- `datahub_freshness_assertion` and `datahub_field_assertion`: a conditionally-required attribute can now be supplied from a variable, a module output, or another resource's attribute. Previously `terraform plan` failed with `Missing fixed_interval_unit`, `Missing cron_schedule`, `Missing cron_timezone`, `Missing metric` or `Incomplete fail threshold` -- pointing at the very line that supplied the value. The presence checks behind those rules read an unresolved value as absent, so an attribute the configuration set looked unset. Any module exposing a freshness schedule or a field metric as an input was affected at plan time. `datahub_volume_assertion` and `datahub_sql_assertion` were never affected. A value supplied where it does not belong is still rejected, resolved or not.

- A whitespace-only `gms_url` or `gms_token` is now treated as absent and reported with the same actionable diagnostic as a missing value, instead of being passed to the API client as a malformed endpoint or a bearer token of spaces.

- `datahub_policy`: the `actors` and `resources` blocks can now be built from a variable, a `for_each`, or any other computed expression. Previously any non-literal value failed the plan with `Received unknown value, however the target type cannot handle unknown values`, naming `resources.filter.criteria`, `resources` or `actors` depending on which block was fed. Terraform marks an optional-but-computed attribute as unknown when it cannot resolve that attribute's default itself, which is what happens when the object you supply omits it - `actors` has three such attributes, `resources` has `all_resources`, and each filter criterion has `condition` - and the provider's internal representation of those blocks could not carry an unknown. The practical effect was that every nested block had to be written out literally, which rules out expressing a policy scope in a reusable module and is most of the reason to want criteria in the first place. Note this is not new in 0.17.0: `actors` has behaved this way since it shipped, so a module that assembles either block is fixed by this release rather than merely restored. Plan-time validation of the filter now defers on values it cannot yet see, and still reports the same conflicts once they resolve. No configuration change is needed and no state is migrated.

- `datahub_domain`: a domain can now be re-created after a `terraform destroy` that also removed a structured property assigned to it. DataHub's cleanup of a deleted property scans a search index that lags the delete, so the cleanup write can land on a domain that has already gone, resurrecting it as an empty "husk" - an entity with nothing but its key aspect, invisible in the UI and in search, yet enough to make the next `terraform apply` fail with "This Domain already exists!" with no domain anywhere for the operator to find and remove. Create now inspects the blocking entity and, only when it provably holds no `domainProperties` aspect and no data beyond an empty structured-properties aspect, removes the husk, retries the create, and reports what it did as a warning. A domain with any real content - including one that merely collides on `domain_id`, and DataHub's separate sibling-name conflict, which reports "already exists" too - is never touched and its original error reaches the user unchanged. `datahub_glossary_node` and `datahub_glossary_term` have had this repair since 0.16.0; domains, the entity type most often paired with structured properties, were the remaining gap.

### Upgrade notes

- **A configuration that supplied no credentials at all now fails.** If your provider block sets neither `gms_url` nor `gms_token`, and `DATAHUB_GMS_URL`/`DATAHUB_GMS_TOKEN` are not in the environment, `terraform plan` now stops with a diagnostic instead of authenticating from `~/.datahubenv`. Set them in the provider block, or export them -- including from the CLI's own values if that is what you want: `DATAHUB_GMS_URL=$(yq '.gms.server' ~/.datahubenv) DATAHUB_GMS_TOKEN=$(yq '.gms.token' ~/.datahubenv)`. Everything else in this release is additive.

## [0.17.0] - 2026-07-29

### Added

- `datahub_organization_display_preferences` resource and data source: manage the organization name and logo that brand the DataHub UI for every user (**Settings -> Preferences** in the UI). DataHub Cloud only -- the backing mutation, its read surface, and the privilege gating it do not exist in open-source DataHub, so the resource fails with a clear diagnostic there rather than a raw schema error. These are org-wide platform settings; the language selector on the same settings page is a per-user preference and is deliberately not managed. This is the provider's first singleton resource: DataHub stores the settings once per instance, so there is no id to supply, applying updates the existing settings rather than creating anything, and `terraform destroy` resets the managed fields to DataHub's defaults instead of deleting anything. The resource owns every field it exposes -- omitting an attribute, setting it to an empty string, or destroying resets that field to the default branding, because DataHub provides no way to remove a value once written (an empty value falls back to the default title and logo, so the effect matches). Reads use the strongly-consistent OpenAPI v3 entity endpoint and writes are read-back verified.
- `datahub_policy`: new `resources.filter` block scoping a METADATA policy by criteria instead of a single entity type and URN list. Each criterion names an entity field (`TYPE`, `URN`, `OWNER`, `DOMAIN`, `GROUP_MEMBERSHIP`, `DATA_PLATFORM_INSTANCE`, `TAG`, `CONTAINER`, `GLOSSARY`), a set of values, and a condition (`EQUALS`, `STARTS_WITH`, `NOT_EQUALS`). Criteria combine with AND and the values within one criterion combine with OR, so a scope like "datasets and containers carrying the `source:snowflake` tag" - previously inexpressible, since `resources.type` takes a single type and there was no tag predicate at all - is now two criteria. There is no top-level OR; a scope spanning unrelated predicates still needs one policy each. `resources.filter` is the form the DataHub UI writes and mirrors the `dataHubPolicyInfo` aspect field-for-field, so a policy built in the UI can be transcribed straight into HCL. The existing `resources.type` / `resources.resources` / `resources.all_resources` attributes are the form DataHub has deprecated; they keep working, and the provider now rejects a config that sets them alongside `filter` at plan time, because DataHub accepts both without complaint and then evaluates only the filter - leaving a policy scoped more broadly than it reads.

### Fixed

- `datahub_policy`: a policy whose resource scope used the criteria filter form - which is what the DataHub UI produces - lost that scope entirely when read back. The provider discarded `resources.filter` while decoding the `dataHubPolicyInfo` aspect, so importing such a policy produced state with an empty resource scope and the next apply would widen the policy to every resource. Refresh of a Terraform-managed policy had the same effect. The filter now round-trips, and the scope survives `terraform import`.
- Provider-level `defaults.tags`: a tag URN that does not exist in DataHub no longer leaves an orphaned entity behind. The existence check ran *after* the resource had already been written to DataHub, so `Create` wrote the entity, failed the tag check, and returned an error without setting state - leaving Terraform with no state entry, `destroy` with nothing to remove, and the entity stranded on the server where neither plan nor state could see it. The next apply with a corrected tag then failed with "already exists". Affected every resource that supports `defaults.tags`: `datahub_corp_user`, `datahub_corp_group`, `datahub_service_account`, `datahub_data_product`, and all six assertion resources. The check now runs before any write, so a bad tag fails with no side effects. `Update` is unchanged and did not need this: the entity is already in state there, so a failed update is an ordinary retryable error rather than an orphan.

### Upgrade notes

- The `resources.filter` addition is additive: existing configurations using `resources.type` / `resources.resources` / `resources.all_resources` are unaffected, and prior state upgrades with no schema version bump and no plan churn (covered by `TestAcc_Policy_UpgradeFromPublished`, which applies with the last published provider and then re-plans with the new one).
- One case does gain a plan diff, by design. A `datahub_policy` whose **server-side** scope carries a criteria filter that the configuration does not declare - typically one imported from the DataHub UI, or one edited in the UI after Terraform created it - now shows that filter in state and therefore plans to remove it. Under earlier versions the filter was invisible to the provider and the next apply silently widened the policy to every resource; the diff is that latent widening becoming visible before it happens. Add the filter to the configuration to keep the scope.

## [0.16.0] - 2026-07-27

### Added

- Provider-level defaults: a new `defaults` provider block (`custom_properties`, `tags`, `structured_properties`) attaches default labels to every resource whose entity type supports them, similar in spirit to the AWS provider's `default_tags`. Resource-level values win over provider defaults on a per-key basis, with a plan-time warning when a differing value collides. Ownership varies by mechanism: `defaults.custom_properties` merges into the existing full-map `custom_properties_all` on `datahub_domain`, `datahub_glossary_term`, `datahub_glossary_node`, `datahub_corp_user`, `datahub_service_account`, and `datahub_data_product`; `defaults.tags` owns the complete `globalTags` list on `datahub_corp_user`, `datahub_service_account`, `datahub_corp_group`, `datahub_data_product`, and all six assertion resources, guarded by an ownership latch so the provider neither reads nor writes an entity's tags until `defaults.tags` is first set (a provider upgrade alone never touches existing UI-applied tags); `defaults.structured_properties` manages only the property URNs it names (per-property ownership, exposed as `structured_properties_defaults`) on `datahub_domain`, `datahub_glossary_term`, `datahub_glossary_node`, `datahub_corp_user`, `datahub_service_account`, `datahub_corp_group`, `datahub_data_product`, and `datahub_data_contract`, applied only to resources whose property definition declares that entity type - so it coexists with `datahub_structured_property_assignment` managing a different property on the same entity, with a plan-time warning if the same property URN is managed both ways. Referenced tags and structured-property definitions must already exist (create them in a prior apply); a missing structured-property definition warns and skips rather than erroring, so `terraform destroy` is never blocked.
- Auto-properties: a `managed-by = "terraform"` custom property is now written automatically to every newly created resource whose entity type supports custom properties, controlled by the top-level `auto_properties` (default `["managed-by"]`; also accepts `provider-version`) and `auto_property_strategy` (`CREATION_ONLY`, the default, stamps and freezes the value at creation so upgrading the provider never produces diffs on existing resources; `PROACTIVE` enforces the current value on every managed entity on every apply, useful for a one-time convergence pass on an estate created before this feature). Set `auto_properties = []` to disable entirely.
- New "Provider-level defaults" guide (`docs/guides/provider-defaults.md`) covering the support matrix, precedence and collision rules, ownership and latch semantics, the `auto_property_strategy` upgrade fence, bootstrap and destroy ordering, a search-filterable marker recipe, and the provider-alias opt-out for excluding specific resources.
- `datahub_structured_property_assignment`: three new supported target entity types - `corpuser` (including service accounts), `corpGroup`, and `dataContract` - alongside the existing `domain`, `glossaryNode`, `glossaryTerm`, and `dataProduct`.
- `examples/runnable/remote-executor-azure`: a runnable example standing up a complete DataHub Cloud Remote Executor deployment from nothing in a single `terraform apply` - a non-default Remote Executor pool, an AKS cluster with the Key Vault secrets-store CSI addon, an Azure Key Vault, a storage account, the `datahub-executor-worker` Helm chart, and an `abs` ingestion source whose recipe exercises both a DataHub-managed secret (resolved via GMS) and a Key Vault secret (file-mounted by the CSI driver). Creates billable Azure infrastructure; the README carries a cost warning and destroy instructions.

### Changed

- `datahub_structured_property_assignment`: the `values` attribute is now a **set** of strings instead of a list. DataHub treats a property's assigned values as an unordered collection - it does not preserve or act on their order (validation is by cardinality count and allowed-value membership) - so modelling `values` as an ordered list produced spurious "update in-place" diffs whenever the server returned the values in a different order than the configuration listed them. As a set, reordering the values is a no-op. This is a schema change on an attribute shipped in 0.14.0, but list and set serialize identically in state (a JSON array), so existing state upgrades cleanly with no migration and no plan churn beyond the one-time removal of any pending reorder diff.

### Fixed

- `datahub_glossary_node` / `datahub_glossary_term`: destroying a structured property together with entities that carried it (a single `terraform destroy` of the `structured-and-custom-properties` example did exactly this) could leave an invisible "husk" entity behind - the entity reappears in DataHub's database with only a key aspect, renders nowhere in the UI or search, and blocks the next `terraform apply` with an "already exists" error. Root cause: DataHub's structured-property-deletion cleanup scrolls the eventually-consistent search index for entities carrying the property and patches each hit, and a stale index read can list an entity that was just hard-deleted, resurrecting it. The provider now settles on a zero-count barrier before issuing a structured-property delete, and separately detects and repairs a husk on the next glossary node/term create (only when the blocking entity is provably empty beyond its key aspect; anything with real content surfaces the original error unchanged). A server-side fix is tracked upstream; both resource docs gain an "Orphaned-husk repair" section.

## [0.15.0] - 2026-07-08

### Added

- `datahub_data_contract` resource: create and manage a DataHub data contract -- a per-dataset bundle that groups existing assertions into freshness, schema, and data-quality guarantees plus a lifecycle `state` (`ACTIVE`/`PENDING`). It is the declarative "this dataset's SLA is X" object; it references assertions authored by the typed `datahub_*_assertion` resources (via `freshness_assertion_urns`/`schema_assertion_urns`/`data_quality_assertion_urns`) rather than creating them, and owns the complete lists (assertions removed here are unbound on the next apply). Works on both OSS DataHub and DataHub Cloud. There is one contract per dataset: the URN is `urn:li:dataContract:<id>` where `id` defaults to a deterministic hash of `dataset_urn` matching the DataHub Python SDK, so a Terraform-managed contract and an SDK-created one for the same dataset are the same entity. Written via the GraphQL `upsertDataContract` mutation; read from the strongly-consistent OpenAPI v3 entity endpoint; deleted via the OpenAPI v3 entity DELETE (no `deleteDataContract` mutation exists), which is a clean hard delete.
- `datahub_data_contracts` data source: return the URNs of all data contracts for bulk import via `for_each` into `import {}` blocks. Backed by `searchAcrossEntities`.
- `examples/runnable/structured-and-custom-properties`: a runnable example contrasting custom properties and structured properties on glossary entities - a term carrying flat `custom_properties` plus a structured property defined once and assigned across two entity types (glossary node + term), with an allowed value left unassigned and both structured properties sharing a dotted qualified-name prefix to show the UI grouping.

### Fixed

- `datahub_structured_property_assignment`: assigning several structured properties to the **same** entity in a single apply could silently drop some of the values. DataHub's `upsertStructuredProperties` mutation is a non-atomic read-modify-write of the entity's single structured-properties aspect, so the provider's per-property writes - which Terraform runs in parallel - raced server-side and lost updates, returning success with no error. The provider now serializes structured-property writes per target entity within a run, which removes the race; assignments to different entities still run fully in parallel. A server-side fix is tracked upstream.

## [0.14.0] - 2026-07-07

### Added

- `custom_properties` on `datahub_glossary_node`, `datahub_glossary_term`, `datahub_corp_user`, and `datahub_service_account`: a key-value string map stored on each entity's info aspect (`glossaryNodeInfo`/`glossaryTermInfo`/`corpUserInfo`), matching the `custom_properties` already on `datahub_domain` and `datahub_data_product`. Glossary nodes and terms render these properties in the DataHub UI, so the attribute gives a first-class home for governance metadata that previously had to be forced into the description. Terraform owns the complete map (keys added elsewhere are removed on the next apply), and the shared validator rejects empty maps, empty keys, and null or empty-string values at plan time. For glossary, the GraphQL create mutations do not carry `customProperties`, so the map is written via the OpenAPI v3 entity endpoint, passing the required aspect fields (`name`/`definition`, and `termSource` for terms) through so they are not clobbered; for users and service accounts it rides along in the existing `corpUserInfo` OpenAPI upsert.
- `datahub_structured_property_assignment` resource: assigns a structured property's value(s) to a target entity. Each resource is one `(entity, property)` edge (`entity_urn`, `structured_property_urn`, `values`), so multiple assignments can target the same entity - one per property - without clobbering each other: writes go through the per-property MERGE `upsertStructuredProperties` mutation, deletes through `removeStructuredProperties`, and read-back through the strongly-consistent OpenAPI v3 entity endpoint. This is the DataHub-native way to attach visible, governed metadata to platform entities (notably domains, which surface structured properties rather than custom properties). Values are string-typed in config; for a `number` property give the number in minimal string form (e.g. `"30"`). The value union, cardinality, and allowed-values are validated server-side. Supported targets are the platform-governance entities `domain`, `glossaryNode`, `glossaryTerm`, and `dataProduct`; other types (ingested data assets) are rejected at plan time - both because per-asset enrichment is out of the provider's scope and because DataHub silently no-ops such writes. The property definition's `entity_types` applicability is also enforced client-side at apply, since DataHub does not enforce it server-side.

## [0.13.0] - 2026-07-06

### Added

- `datahub_service_account` resource: manages a DataHub service account (a non-human identity for CI/CD, ingestion, and automation). Requires DataHub Core >= 1.4.0 or DataHub Cloud, and the `Manage Users & Groups` privilege. A service account is a `corpUser` carrying a `SERVICE_ACCOUNT` subtype under a `service_` URN prefix; the resource takes a user-supplied `service_account_id` and writes the `corpUserKey`, `corpUserInfo`, and `subTypes` aspects via OpenAPI v3, yielding a deterministic `urn:li:corpuser:service_<service_account_id>`. It deliberately does not call the GraphQL `createServiceAccount` mutation, which mints a random UUID id incompatible with Terraform's declarative model (the same UUID-bypass used by `datahub_ownership_type` and `datahub_domain`). Read and import are subtype-guarded: the resource refuses to manage a `corpUser` that is not a service account. Access tokens are minted separately (Settings -> Access Tokens) and are not managed here.
- `datahub_service_account` data source: looks up a service account by `service_account_id`, returning its URN and profile. Fails if the id resolves to a `corpUser` that is not a service account.
- `datahub_service_accounts` data source: returns the URNs of all service accounts (via the `listServiceAccounts` GraphQL query), for feeding an `import {}` for-each block to bulk-import existing service accounts. Eventually consistent - use for enumeration, not authoritative reads.
- `datahub_assertion_assignment_rule` resource: create and manage a DataHub Cloud assertion assignment rule -- a declarative rule that auto-assigns freshness and/or volume monitors to every dataset matching a search filter. One rule replaces hand-authoring a per-dataset assertion across many datasets: as new datasets match the filter, they are monitored automatically. Targets are expressed with `or_filters` (a disjunction of AND-groups of `{field, values, condition, negated}` facet predicates, mirroring DataHub's search filter model); optional `freshness` and `volume` blocks enable each monitor category with a `source_type` and incident `on_success_actions`/`on_failure_actions`. The URN is deterministic: `rule_id` (a URN suffix, derived from `name` when omitted) produces `urn:li:assertionAssignmentRule:<rule_id>`, created via `createAssertionAssignmentRule` and read from the strongly-consistent OpenAPI v3 entity endpoint. Requires DataHub Cloud; returns a clear diagnostic on OSS DataHub.
- `datahub_assertion_assignment_rules` data source: return the URNs of all DataHub Cloud assertion assignment rules for bulk import via `for_each` into `import {}` blocks. Backed by the `listAssertionAssignmentRules` GraphQL query. Requires DataHub Cloud.

### Fixed

- `datahub_structured_property`: allowed values containing a `/` (or `~`) now write successfully. The create/update path previously used the GraphQL `createStructuredProperty`/`updateStructuredProperty` mutations, whose server-side JSON-Patch builder splices each allowed value into an unescaped RFC-6901 JSON Pointer path - so a value like `SITS/eVision` was parsed as nested path segments and the write failed with `Invalid format for aspect: structuredProperty ... /allowedValues/0/value :: field is required` (a DataHub server bug affecting both OSS and Cloud). The provider now writes the structured-property definition (and settings) aspect via the OpenAPI v3 entity endpoint, which has no patch step and stores such values correctly; this also aligns the write path with the resource's existing OpenAPI read path and with how `datahub_domain`, `datahub_tag`, and `datahub_data_product` are already written. The update path, previously sent as GraphQL deltas, now writes the full desired definition - safe because the plan modifiers force replacement (not update) on any list shrink or cardinality narrowing.

## [0.12.0] - 2026-07-02

### Added

- `datahub_domain` resource and data source: added `custom_properties`, a key-value string map stored on the domain's `domainProperties` aspect. Domains support arbitrary custom properties in DataHub, but the resource previously exposed only `name`, `description`, and `parent_domain`, forcing that metadata into the description. Terraform owns the complete map (keys added elsewhere are removed on the next apply), mirroring `datahub_data_product`. Because the GraphQL `createDomain`/`updateName`/`updateDescription` mutations do not carry `customProperties`, the map is written via the OpenAPI v3 entity endpoint, passing `name`/`description`/`parentDomain` through so the values those mutations own are preserved.
- `examples/runnable/financial-services`: expanded the FIBO example into an end-to-end financial-services governance scenario. A `make`-driven Python pipeline downloads ISO 20022 message schemas from [iso20022.org](https://www.iso20022.org) and emits roughly 800 message types as Kafka topics, PostgreSQL tables, and Looker views with three-tier lineage; an LLM tagging pass maps each dataset and its individual columns to FIBO domains and glossary terms (for example the `Dbtr` and `Cdtr` columns of a pacs.008 credit transfer to the Debtor and Creditor terms); and a generated `assertions.tf` applies schema, volume, field, SQL, and freshness assertions across 26 representative tables. A `DEMO.md` runbook documents the resulting search, lineage, tagging, and data-quality navigation paths, each verified against a live instance.

### Changed

- `datahub_data_product`: `custom_properties` now rejects an empty map, empty keys, null values, and empty-string values at plan time. Previously these were silently coerced (a null value became `""`) or produced perpetual drift (an empty map read back as `null`). This is a behaviour change: a configuration that set any of those will now fail at plan with an actionable error - fix it by populating or removing the offending key, or omitting the attribute entirely. The same rule now applies to the new `datahub_domain.custom_properties`; both share one validator.
- Renamed the `examples/runnable/domain-hierarchy-fibo` example to `examples/runnable/financial-services`. The example outgrew its original domain-hierarchy scope - it now spans payments, securities, FX, collateral, and trade-finance message types with glossary, column-level tagging, lineage, and data-quality layers - so it is named for its industry vertical rather than its initial FIBO-domains contents. (Anyone referencing the old path should update it; the released 0.9.0 entry below keeps the old name as historical record.)
- `examples/runnable/financial-services`: the generated `assertions_config.json` is no longer committed. It is a ~7,000-line artifact rebuilt from the ISO 20022 cache by `make iso-assertions-config` into the gitignored `.iso-cache/` directory, and `assertions.tf` reads it through a `fileexists` guard that plans zero assertions when the file is absent (matching the FIBO cache behaviour in `main.tf`).
- README: documented the typed assertion resources (`datahub_schema_assertion`, `datahub_volume_assertion`, `datahub_field_assertion`, `datahub_sql_assertion`, `datahub_freshness_assertion`, `datahub_custom_assertion`), `datahub_action_pipeline`, `datahub_data_product`, and `datahub_ownership_type`, and their data sources, in the "What it supports" tables - these shipped in earlier releases but were missing from the README.

## [0.11.0] - 2026-06-29

### Added

- `datahub_action_pipeline` resource: create and manage a DataHub Cloud action pipeline (automation) -- a packaged action that runs a recipe to propagate metadata (descriptions, tags, glossary terms) back to a platform such as BigQuery or Dataplex. The resource manages the pipeline definition (`name`, `type`, `recipe`, `category`, `description`, `executor_id`, `version`, `debug_mode`); the `recipe` is a JSON string compared by semantic equality (like `datahub_ingestion_source`), and `${SECRET_NAME}` placeholders are stored verbatim and resolved at execution time. The URN is deterministic: `action_id` (a URN suffix, derived from `name` when omitted) produces `urn:li:dataHubAction:<action_id>`, written via `upsertActionPipeline`. Requires DataHub Cloud; returns a clear diagnostic on OSS DataHub.
- `datahub_action_pipelines` data source: return the URNs of all DataHub Cloud action pipelines for bulk import via `for_each` into `import {}` blocks. Backed by the `listActionPipelines` GraphQL query. Requires DataHub Cloud.

### Changed

- `datahub-tf-extract`: fails fast when the `--output` directory already contains a `generated.tf` from a previous run -- a re-run previously left stale, partial config in place while still reporting success, because `terraform plan -generate-config-out` refuses to overwrite the file. A genuine validation-plan failure is now fatal rather than a swallowed warning.
- Import guide (`docs/guides/import-existing.md`): corrected the write-only resource note (the first post-import apply plans a one-time replacement, not a no-op, because `*_wo_version` imports as null), noted that shared-instance narrowing bypasses the tool's post-processing, and added a "Migrating from another Terraform provider" section for provider-swap migrations.

## [0.10.0] - 2026-06-18

### Added

- `datahub_field_assertion` resource: create and manage a DataHub field (column) assertion monitor. Two sub-types: `FIELD_VALUES` checks every row's value against an `operator` and value (with optional `transform_type` such as `LENGTH`, a `fail_threshold_type`/`fail_threshold_value` tolerance, and `exclude_nulls`), and `FIELD_METRIC` checks an aggregate column metric (one of 17 kinds: `NULL_COUNT`, `UNIQUE_COUNT`, `MIN`, `MAX`, `MEAN`, `STDDEV`, etc.). The column is described by `field_path`/`field_type`/`field_native_type`. A plan-time validator enforces the sub-type split (`metric` required for `FIELD_METRIC`, rejected for `FIELD_VALUES`). `FIELD_VALUES` requires a warehouse-backed platform (BigQuery, Snowflake, Redshift, Databricks) and a query `source_type`; `FIELD_METRIC` can evaluate against a previously ingested dataset profile. Requires DataHub Cloud; returns a clear diagnostic on OSS DataHub. URN is server-generated.
- `datahub_schema_assertion` resource: create and manage a DataHub schema assertion monitor. Asserts that a dataset's columns match an expected set, catching unexpected schema drift, with a `compatibility` mode (`EXACT_MATCH`, `SUPERSET`, `SUBSET`). The resource owns the complete expected `fields` list (each `path`/`type`/`native_type`). On read DataHub returns each field's standard type as a `SchemaFieldDataType` class object rather than the plain string sent on write; the provider maps the class back to the standard type so the resource stays drift-free. Requires DataHub Cloud; returns a clear diagnostic on OSS DataHub. URN is server-generated.
- `datahub_volume_assertion`: `ROW_COUNT_CHANGE` (growth) sub-type, selected via `volume_type = "ROW_COUNT_CHANGE"` plus a new `change_type` attribute (`ABSOLUTE` or `PERCENTAGE`), alongside the existing `ROW_COUNT_TOTAL`. The operator and value attributes are reused, so all NATIVE volume assertions are now expressible in one resource.
- `datahub_sql_assertion`: `METRIC_CHANGE` sub-type, selected via `sql_type = "METRIC_CHANGE"` plus a `change_type` attribute (`ABSOLUTE` or `PERCENTAGE`). `METRIC_CHANGE` requires a non-empty `description` (DataHub rejects the mutation otherwise), enforced at plan time.
- `datahub_freshness_assertion`: `SINCE_THE_LAST_CHECK` schedule type, which asserts the dataset changed at all between consecutive evaluations and takes no window sub-configuration, alongside `FIXED_INTERVAL` and `CRON`. A new config validator ties the window sub-fields to the chosen `schedule_type`.
- Cross-cutting assertion inputs: `description` on `datahub_volume_assertion` and `datahub_freshness_assertion` (`datahub_sql_assertion` already had it); `filter_sql` (a row-level SQL filter) on `datahub_volume_assertion` and `datahub_freshness_assertion`; and `failure_severity` (`LOW`/`MEDIUM`/`HIGH`) on the freshness, sql, and field assertions.
- `datahub-tf-extract` and the `datahub_assertions` data source now enumerate the new `datahub_field_assertion` and `datahub_schema_assertion` resources for bulk import.

### Changed

- `datahub-tf-extract`: hardened enumeration and unified the import-target registry so the CLI enumerates every importable resource type (previously only a subset), with system-source filtering and URN de-duplication.
- Assertion enumeration and import are now scoped to NATIVE (author-as-code) assertions. Ingested `EXTERNAL` assertions (e.g. dbt tests, Great Expectations) and smart/AI `INFERRED` assertions are never enumerated, and a direct `terraform import` of a non-NATIVE assertion into a typed resource is refused with a clear diagnostic, since those are owned and regenerated by the producing system.
- `datahub_ingestion_source`: the `recipe` attribute now uses JSON semantic equality (`jsontypes.Normalized`), so key ordering and whitespace differences no longer produce spurious plan diffs. Existing state is normalized once on the next apply.
- Go toolchain updated to 1.26.4; pinned `mise` tool versions refreshed.

### Fixed

- ImportState of `datahub_freshness_assertion`, `datahub_volume_assertion`, and `datahub_sql_assertion` now recovers the monitor-side fields (evaluation schedule, source type, and mode) from the associated Monitor entity. Previously these were not read back, so an imported assertion produced a spurious diff on the first plan; imports now re-plan cleanly.

## [0.9.0] - 2026-06-11

### Added

- `datahub_custom_assertion` resource: create and manage custom (external) DataHub assertion definitions. Custom assertions are evaluated by an external system (dbt tests, Great Expectations, custom scripts) and reported back to DataHub via `reportAssertionResult`. The resource declares the assertion and associates it with a dataset URN; it does not execute the assertion itself. Works on both OSS DataHub and DataHub Cloud. DataHub generates a server-side UUID for the URN on first creation; the provider stores and reuses it on all subsequent updates, passing the existing URN back to the `upsertCustomAssertion` mutation to guarantee idempotent upserts.
- `datahub_freshness_assertion` resource: create and manage a DataHub freshness assertion monitor. Freshness assertions check that a dataset has been updated within an expected window, evaluated on a configurable cron schedule. Supports `FIXED_INTERVAL` (rolling window, e.g. data must arrive every 24 hours) and `CRON` (calendar window) schedule types. Requires DataHub Cloud; returns a clear diagnostic on OSS DataHub. URN is server-generated.
- `datahub_volume_assertion` resource: create and manage a DataHub volume assertion monitor. Volume assertions check that a dataset has an expected row count at evaluation time. Supports `DATAHUB_DATASET_PROFILE` source type (evaluates against a previously ingested DatasetProfile - no live database query required), `INFORMATION_SCHEMA`, and `QUERY`. Requires DataHub Cloud; returns a clear diagnostic on OSS DataHub. URN is server-generated.
- `datahub_sql_assertion` resource: create and manage a DataHub SQL assertion monitor. SQL assertions run a custom SELECT statement and compare the numeric result to an expected value, enabling business-logic checks (no negative values, referential integrity counts, etc.) that volume and freshness assertions cannot express. Requires DataHub Cloud; returns a clear diagnostic on OSS DataHub. URN is server-generated.
- `datahub_assertion` data source: look up an existing DataHub assertion by URN and return its type and target entity URN. Use this to reference an assertion created outside Terraform without taking ownership of it.
- `datahub_assertions` data source: return the URNs of all DataHub assertions for bulk import via `for_each` into `import {}` blocks. Backed by `searchAcrossEntities` (OpenSearch).
- `examples/runnable/assertion-volume-sqlite`: runnable example demonstrating a volume assertion evaluated against a locally-seeded SQLite dataset profiled via the DataHub CLI. Includes a Python seed script and a README walkthrough of the PASS-FAIL-PASS cycle using `DATAHUB_DATASET_PROFILE` source type (no live database query from DataHub Cloud).

### Fixed

- `datahub_custom_assertion` destroy on OSS DataHub: the `deleteAssertion` GraphQL mutation rejects CUSTOM type with "Unsupported Assertion Type CUSTOM". The provider now falls back to the OpenAPI v3 entity endpoint for CUSTOM type deletes, which works on both OSS and Cloud.
- `datahub_custom_assertion` resource and `datahub_assertion` data source: `entity_urn` was empty when reading assertions from OSS DataHub. OSS assertionInfo schema v3 stores the entity URN inside `customAssertion.entity` rather than the top-level `entityUrn` field added in Cloud schema v4. Both read paths now fall back to `customAssertion.entity` when `entityUrn` is absent.
- `datahub_freshness_assertion`, `datahub_volume_assertion`, `datahub_sql_assertion`: an immediate update after create on DataHub Cloud could fail with "Monitor for assertion X does not exist." DataHub Cloud creates the linked monitor entity asynchronously after the upsert mutation returns; the provider now polls until the monitor is visible before returning from Create.
- `examples/runnable/domain-hierarchy-fibo`: added a `terraform plan`-time precondition that fails with a clear message (`FIBO cache is missing or stale. Run: make fibo-update`) when the `.fibo-cache/fibo.json` file is absent or was generated by an older version of the build script. Previously, a missing cache produced a confusing Terraform type-consistency error with no actionable guidance.

## [0.8.0] - 2026-06-08

### Added

- `datahub_data_product` resource: create and manage DataHub data product definitions with a deterministic, user-supplied `data_product_id` (URN suffix). Manages the product definition -- `name`, `description`, optional `domain` (full domain URN), `external_url`, and `custom_properties` -- but not asset membership. Member datasets, charts, and other assets are intentionally out of scope: asset membership is per-asset enrichment and is managed via the DataHub UI, CLI, or SDK without interference from `terraform apply`. Create and update write the `dataProductProperties` and `domains` aspects directly via the OpenAPI v3 endpoint, not the GraphQL mutations, because `createDataProduct`/`updateDataProduct` cannot set `external_url` or `custom_properties`. The DataHub UI creates data products with a random UUID when no id is supplied; the provider requires an explicit `data_product_id` to produce stable, importable URNs that match the DataHub Python SDK convention (`make_data_product_urn(id)`).
- `datahub_data_product` data source: look up an existing data product by `data_product_id` and return its URN, name, description, domain, external URL, and custom properties. Use this to reference a data product created outside Terraform without taking ownership of it.
- `datahub_data_products` data source: return the URNs of all DataHub data products for bulk import via `for_each` into `import {}` blocks. Backed by the `searchAcrossEntities` GraphQL query (entity type `DATA_PRODUCT`).
- `examples/runnable/data-product-simple`: runnable example creating a domain and two data products (Orders, Customer 360), demonstrating the resource with custom properties, the singular data source, and the plural list data source.
- `datahub_ownership_type` resource: create and manage custom DataHub ownership type definitions with a deterministic, user-supplied `type_id` (URN suffix). Ownership types are named roles assigned to asset owners (e.g. "Data Quality Lead", "Data Producer") visible throughout the DataHub UI. Create and update write the `ownershipTypeInfo` aspect directly via the OpenAPI v3 endpoint - the GraphQL `createOwnershipType` mutation is not used because it generates a server-side random UUID for the id, making URNs non-deterministic and unmanageable by Terraform. This matches the DataHub Python SDK convention (`make_ownership_type_urn(id)`). The four built-in system types (`__system__technical_owner`, `__system__business_owner`, `__system__data_steward`, `__system__none`) cannot be managed or deleted; `type_id` values beginning with `__system__` are rejected at plan time.
- `datahub_ownership_type` data source: look up an existing ownership type by `type_id` and return its URN, name, and description. Works for both custom types and built-in system types. Use this to reference a built-in type's URN (e.g. `__system__technical_owner`) without taking ownership of it.
- `datahub_ownership_types` data source: return the URNs of all DataHub ownership types (custom and system) for bulk import via `for_each` into `import {}` blocks. Backed by the `listOwnershipTypes` GraphQL query.
- `examples/runnable/ownership-type-simple`: runnable example creating two custom ownership types (Data Quality Lead, Data Producer), reading back a custom type and the built-in Technical Owner type via the singular data source, and enumerating all ownership types via the plural data source.

## [0.7.0] - 2026-06-07

### Added

- `datahub_structured_property` resource: create and manage DataHub structured property definitions with a deterministic, user-supplied `property_id` (URN suffix and `qualifiedName`). Manages the property schema -- `value_type` (`string`, `number`, `date`, `urn`, `rich_text`), `cardinality` (`SINGLE`/`MULTIPLE`), `entity_types` (which asset types the property can be applied to), optional `allowed_values` enum constraint, optional `allowed_entity_types` filter for `urn`-typed properties, and the `structuredPropertySettings` display-flag aspect via a `settings {}` block. This resource manages the definition only; applying values to individual assets is per-asset enrichment and is out of scope. The DataHub `updateStructuredProperty` mutation is append-only for list fields (`entity_types`, `allowed_values`, `allowed_entity_types`) and cardinality can only widen `SINGLE`->`MULTIPLE`: additive changes are applied in-place, while removing an element or narrowing cardinality forces resource replacement (which hard-deletes the property and removes all applied values from assets).
- `datahub_structured_property` data source: look up an existing structured property definition by `property_id` and return all fields. Use this to reference a property created outside Terraform without taking ownership of it.
- `datahub_structured_properties` data source: return the URNs of all DataHub structured properties for bulk import via `for_each` into `import {}` blocks.
- `examples/runnable/structured-property-simple`: runnable example creating a `number`-typed retention-days property (dataset) and a `string`-typed classification property with allowed values (dataset + dashboard), demonstrating the resource, singular data source, and plural list data source.
- `datahub_tag` resource: create and manage DataHub tags with a deterministic, user-supplied `tag_id` (URN suffix). Manages the tag entity itself -- its display `name`, `description`, and optional `color_hex` display colour (`#RRGGBB` format) -- not where the tag is applied to data assets. Tags are flat (no parent/child hierarchy). Create uses the `createTag` GraphQL mutation; colour is set via the dedicated `setTagColor` mutation; renames write the `tagProperties` aspect directly via OpenAPI v3 (the DataHub `updateName` mutation does not support the Tag entity type).
- `datahub_tag` data source: look up an existing tag by `tag_id` and return its URN, name, description, and colour. Use this to reference a tag created outside Terraform without taking ownership of it.
- `datahub_tags` data source: return the URNs of all DataHub tags for bulk import via `for_each` into `import {}` blocks.
- `examples/runnable/tag-simple`: runnable example creating three tags (PII, Verified, Deprecated) with distinct colours, demonstrating the resource, singular data source, and plural list data source.

## [0.6.0] - 2026-06-06

### Added

- `datahub_glossary_node` resource: create and manage DataHub glossary nodes (the "Term Groups" shown in the DataHub UI) with a deterministic, user-supplied `node_id` (URN suffix). Nodes can be nested to any depth via an optional `parent_node` attribute. Set `parent_node` to another `datahub_glossary_node` resource's `.urn` attribute so Terraform's dependency graph creates parents before children and destroys children before parents. Unlike domains, DataHub does not refuse to delete a node that still has children, so correct ordering via `.urn` references is the only ordering guarantee. Reparenting is performed in place via `updateParentNode` without forcing replacement.
- `datahub_glossary_node` data source: look up an existing term group by `node_id` and return its URN, name, description, and parent node. Use this to reference an unmanaged node as a `parent_node` input without taking ownership of it.
- `datahub_glossary_nodes` data source: return the URNs of all DataHub glossary nodes for bulk import via `for_each` into `import {}` blocks.
- `datahub_glossary_term` resource: create and manage DataHub glossary terms (the "Terms" shown in the DataHub UI) with a deterministic, user-supplied `term_id` (URN suffix, max 56 characters). Terms live under a `datahub_glossary_node` via the `parent_node` attribute; terms cannot be parents of other terms. Reparenting (including detaching to root) is performed in place via `updateParentNode` without forcing replacement.
- `datahub_glossary_term` data source: look up an existing term by `term_id` and return its URN, name, description, and parent node.
- `datahub_glossary_terms` data source: return the URNs of all DataHub glossary terms for bulk import via `for_each` into `import {}` blocks.
- `domain` attribute on `datahub_glossary_node` and `datahub_glossary_term`: associate a glossary entity with a DataHub domain by setting this attribute to a domain URN (e.g. `datahub_domain.finance.urn`). The association is managed via the `setDomain`/`unsetDomain` GraphQL mutations and is read back from the `domains` aspect on the strongly-consistent OpenAPI v3 endpoint.

### Changed

- Extracted the `updateName` and `updateDescription` GraphQL mutation wrappers into shared client helpers (`UpdateEntityName`, `UpdateEntityDescription`) reused by domains, glossary nodes, and glossary terms.
- Extracted `SetEntityDomain` and `UnsetEntityDomain` as shared client helpers wrapping the `setDomain`/`unsetDomain` GraphQL mutations, available for reuse by future resources that support domain association.

## [0.5.0] - 2026-06-05

### Added

- `datahub_domain` resource: create and manage DataHub domains with a deterministic, user-supplied `domain_id` (URN suffix). Domains can be nested to any depth via an optional `parent_domain` attribute. Set `parent_domain` to another `datahub_domain` resource's `.urn` attribute so Terraform's dependency graph creates parents before children and destroys children before parents — DataHub hard-deletes domains and refuses deletion if any child domains exist. Reparenting is performed in place via `moveDomain` without forcing replacement.
- `datahub_domain` data source: look up an existing domain by `domain_id` and return its URN, name, description, and parent domain. Use this to reference an unmanaged domain (created outside Terraform) as a `parent_domain` input without taking ownership of it.
- `datahub_domains` data source: return the URNs of all DataHub domains across the full hierarchy, for bulk import via `for_each` into `import {}` blocks.

### Fixed

- Import guide template was not updated when the extract tool archive was renamed to `tools-datahub-tf-extract` in v0.4.1, causing `make generate` to revert the already-corrected `docs/guides/import-existing.md` on every run.

## [0.4.1] - 2026-06-04

### Fixed

- Release packaging: the `datahub-tf-extract` zip archives were included in the provider `SHA256SUMS` file and sorted alphabetically before `terraform-provider-datahub`, causing the Terraform Registry to serve the extract tool zip instead of the provider zip. `terraform init` failed with "provider binary not found" on all platforms. Fixed by renaming the extract tool archives to `tools-datahub-tf-extract_*` (which sorts after `terraform-provider-datahub_*`). The binary name inside the zip (`datahub-tf-extract`) is unchanged. Update any `mise.toml` `ubi` entries to use `matching = "tools-datahub-tf-extract"`.

## [0.4.0] - 2026-06-01

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
- `datahub_corp_user` resource: create and manage a DataHub user's catalog profile (`corpUserInfo` aspects) with upsert semantics. Works for new users and pre-existing ones created by SSO/JIT provisioning, metadata ingestion, or `datahub_local_user_login`. Delete hard-deletes the user entity.
- `datahub_corp_user` data source: resolve a `username` to its URN and catalog metadata (display name, email, title, active, status).
- `datahub_local_user_login` resource: provision native-auth login credentials for a DataHub user via the signUp flow. When `initial_password` is omitted, generates a random throwaway password and exposes a single-use 24h reset URL (`password_reset_url`) so the user sets their own password -- Terraform never holds a real credential. Works on both OSS DataHub and DataHub Cloud (on Cloud, `username` must be the user's email address). Delete hard-deletes the entire user entity. Requires Terraform CLI 1.11+.
- `frontend_url` optional provider config: explicit DataHub frontend URL for native user operations. Derived automatically from `gms_url` when not set.
- `examples/runnable/local-iam`: runnable example demonstrating the full IAM stack -- a login user, a catalog-only service/pipeline account, group membership, a role assignment, and an access policy for a team.

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

[Unreleased]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.22.0...HEAD
[0.22.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.21.1...v0.22.0
[0.21.1]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.21.0...v0.21.1
[0.21.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.20.0...v0.21.0
[0.20.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.19.1...v0.20.0
[0.19.1]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.19.0...v0.19.1
[0.19.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.18.0...v0.19.0
[0.18.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.17.0...v0.18.0
[0.17.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.16.0...v0.17.0
[0.16.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.15.0...v0.16.0
[0.15.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.14.0...v0.15.0
[0.14.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.13.0...v0.14.0
[0.13.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.12.0...v0.13.0
[0.12.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.11.0...v0.12.0
[0.11.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.10.0...v0.11.0
[0.10.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/datahub-project/terraform-provider-datahub/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/datahub-project/terraform-provider-datahub/releases/tag/v0.1.0
