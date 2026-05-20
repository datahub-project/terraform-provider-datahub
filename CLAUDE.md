# Terraform Provider for DataHub

Terraform Plugin Framework provider that talks to the DataHub OpenAPI v3 REST surface. Manages DataHub configuration and data objects (Ingestion Sources today; more to follow). Does not provision DataHub infrastructure.

The provider works against the open-source DataHub API and also against DataHub Cloud, since both expose the same OpenAPI surface.

## Target home and donation status

This project is being prepared for donation to the open-source DataHub community. Once published:

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
- Tests: none yet (a known gap; unit tests on `pkg/datahub/` and `pkg/tools/uid/` are low-hanging fruit that don't need a live DataHub instance).

## Pre-release strategy

Targeting an initial `v0.1.0` release rather than `v1.0.0`. The `0.x` prefix is the Terraform Registry's accepted signal for "API is not stable yet"; breaking changes remain permitted until the project chooses to flip to `v1.0`. This avoids the "release-and-immediately-deprecate" pattern.

Pre-release work that is known and open (no decision recorded yet):

- No tests exist. Adding unit tests for at least `internal/provider/pkg/datahub/` and `internal/provider/pkg/tools/uid/` before v0.1.0 would materially raise contributor confidence.

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

When building a runnable example under `examples/`, always include outputs that let the user verify or act on the result of their `terraform apply` without leaving the terminal. At minimum:

- Expose any IDs or URNs that identify the created resource (e.g. `source_id`, `source_urn`).
- Where a follow-up action is natural (triggering a run, querying status, opening a UI page), include the relevant curl/CLI command or URL in the README, referencing the outputs directly via `terraform output -raw <name>`.
- If the DataHub UI is the most natural place to verify the result, include the navigation path and a direct URL template (e.g. `$DATAHUB_GMS_URL/ingestion` for ingestion sources).

The goal: a user who has just applied the example can verify the result and take the logical next step without hunting through docs.

## DataHub domain vocabulary (quick reference)

- **Ingestion Source** - the configured, persisted entity in DataHub that represents one source-of-metadata. Resource-shaped.
- **Recipe** - the YAML/JSON configuration document that *defines* an ingestion source's connector, options, and sinks.
- **ingest** - the verb. Either ad-hoc CLI execution (`datahub ingest -c recipe.yaml`) or the act of running a deployed Ingestion Source.

Keep this distinction in code, docs, and resource naming.
