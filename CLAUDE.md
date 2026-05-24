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

## Build and development

- Go module: `github.com/datahub-project/terraform-provider-datahub`
- Go version: 1.26.3 (pinned in `mise.toml`; declared in `go.mod`)
- Tools submodule: `tools/` (Go 1.24.x; holds `tfplugindocs`)
- Build: `make install` (writes to `./bin/terraform-provider-datahub`)
- Verify: `go build ./...` and `go vet ./...`
- Generate docs: `cd tools && go generate ./...`
- Tests: `make test` (unit + mock acceptance); `make testacc` (live acceptance, requires a running DataHub instance)

## Tool version maintenance

Dependabot has no `mise` ecosystem support — tool versions pinned in `mise.toml` are a blind spot not covered by any automated process.

Before cutting a release (or if `mise.toml` has not changed in a long time), check and update pinned tools:

```bash
mise outdated --local          # check what is stale (--local scopes to this project only)
mise upgrade --bump            # install newer versions and rewrite pins in mise.toml
```

Always use `--local`; without it, global mise tools (e.g. `awscli`) appear as noise.

## Release strategy

The project is on `0.x` versioning. The `0.x` prefix is the Terraform Registry's accepted signal for "API is not stable yet"; breaking changes remain permitted until the project chooses to flip to `v1.0`.

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

## Example conventions

### File layout

Runnable examples follow the standard HashiCorp convention for file separation:

- `main.tf` — provider block and resource/data source declarations only
- `outputs.tf` — all `output` blocks; never mix them into `main.tf`
- `variables.tf` — input variables (add when the example needs parameterisation)
- `README.md` — prerequisites, run instructions, follow-up actions, cleanup

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
