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

## DataHub domain vocabulary (quick reference)

- **Ingestion Source** - the configured, persisted entity in DataHub that represents one source-of-metadata. Resource-shaped.
- **Recipe** - the YAML/JSON configuration document that *defines* an ingestion source's connector, options, and sinks.
- **ingest** - the verb. Either ad-hoc CLI execution (`datahub ingest -c recipe.yaml`) or the act of running a deployed Ingestion Source.

Keep this distinction in code, docs, and resource naming.
