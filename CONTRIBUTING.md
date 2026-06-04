# Contributing to terraform-provider-datahub

Thank you for your interest in contributing. This document covers how to report issues, propose changes, and get your pull request merged.

## Code of conduct

Be respectful and constructive. We follow the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md).

## Reporting issues

Open a GitHub issue for:

- Bugs (unexpected error, crash, incorrect plan output, wrong API call)
- Feature requests (new resource, new data source, attribute gap)
- Documentation gaps or inaccuracies

Before opening, search existing issues in case the same thing has already been reported. For security vulnerabilities, do not open a public issue - contact the maintainers privately via the email in `SECURITY.md` (once that file exists; for now, open a draft PR or email the repo maintainers directly).

## Setting up a development environment

See [BUILDING.md](BUILDING.md) for a full walkthrough. The short version:

```bash
git clone https://github.com/datahub-project/terraform-provider-datahub.git
cd terraform-provider-datahub
mise install
make dev-override
cd .   # re-trigger mise to activate TF_CLI_CONFIG_FILE
```

After that, `terraform` uses your locally-built binary and `datahub` is available from the project venv.

## Making changes

### Branch naming

Use a short descriptive prefix:

- `feat/<topic>` - new feature or resource
- `fix/<topic>` - bug fix
- `docs/<topic>` - documentation only
- `refactor/<topic>` - internal restructuring, no behaviour change
- `test/<topic>` - test additions or fixes

### Commit messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <short summary>

Optional longer body. Explain the *why*, not the what.
```

Common types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`.

### Code style

- Run `make fmt` before committing (`gofmt -s`).
- Run `make lint` if you have `golangci-lint` installed. CI will catch failures.
- Run `make generate` if you change resource schemas, and commit the updated `docs/` output.

### License headers

All new Go files must start with:

```go
// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0
```

Do not modify the copyright header on files that carry the HashiCorp MPL-2.0 notice (listed in `NOTICE`).

## Testing

All contributions must pass `make testacc` (mock-backed acceptance tests, no live DataHub required):

```bash
make test       # unit tests only
make testacc    # full acceptance tests against the in-memory mock
```

For changes that touch provider-DataHub API interaction, also run against a local Quickstart if possible:

```bash
make testacc-quickstart   # boots Quickstart, runs tests, nukes on exit
```

See [BUILDING.md](BUILDING.md) for the full testing matrix and knobs.

## Pull requests

1. Fork the repo and push your branch to your fork.
2. Open a pull request against `main`.
3. Fill out the PR description: what changed, why, and how to test it.
4. Ensure CI is green (build, generate, lint, mock acceptance tests).
5. For new resources or data sources, include generated `docs/` output (`make generate`) and an example under `examples/`.

PRs are squash-merged. The squash commit message is taken from the PR title, so keep it in Conventional Commits format.

### Checklist before requesting review

- [ ] `make fmt` and `make lint` pass
- [ ] `make testacc` passes
- [ ] `make generate` run and `docs/` output committed (if schema changed)
- [ ] New files have the correct license header
- [ ] README / BUILDING.md updated if user-facing behaviour changed

## Adding a new resource or data source

Before writing code, read `docs/design/datahub-model-and-resource-design.md`. It covers URN strategy, reference modeling, upsert semantics, delete behavior, and provider scope. Proposals that skip this step tend to need significant rework.

The quick checklist is also reproduced in `CLAUDE.md` for AI-assisted development.

## Release process

Releases are cut by the maintainers. A `v*` tag push triggers the GoReleaser workflow, which builds multi-platform binaries, signs them with GPG, and publishes to the Terraform Registry. Contributors do not need to do anything special - just get your PR merged to `main`.

### Pre-release dependency audit

This is the single checklist for "are our tools and dependencies up to date?" Run it before opening the prepare PR.

| What | Automation | Manual action |
|---|---|---|
| Go module deps (`go.mod`) | Dependabot opens grouped PRs weekly (Mondays) | Merge any open Dependabot PRs first |
| Go module deps (`tools/go.mod`) | Dependabot opens grouped PRs weekly (Mondays) | Merge any open Dependabot PRs first |
| GitHub Actions version pins | Dependabot opens grouped PRs weekly (Mondays) | Merge any open Dependabot PRs first |
| mise-managed tool versions (Go, Terraform, golangci-lint, ...) | **None** - Dependabot has no mise ecosystem support | Run the commands below |

mise tool pins in `mise.toml` are the only blind spot not covered by any automated process:

```bash
mise outdated --local   # list stale versions scoped to this project (not global mise tools)
mise upgrade --bump     # install newer versions and rewrite pins in mise.toml
```

After `mise upgrade --bump`, run `make test && make testacc` to confirm nothing broke, then include the updated `mise.toml` in the prepare PR.

#### Indirect Go dependency freshness (periodic, not every release)

Dependabot updates **direct** dependencies for all version bumps. For **indirect** (transitive) dependencies it only intervenes on security advisories -- a newer but non-security version of an indirect dep will silently drift unless a direct dep update happens to pull it along.

To see the full picture across every module in the dependency graph:

```bash
go list -u -m all           # read-only: shows current pin and latest available for every module
go list -u -m all 2>/dev/null | grep '\[' | head -20   # just the ones with updates
```

To actually update everything to the latest compatible minor/patch:

```bash
go get -u ./...             # upgrade all direct and indirect deps
go mod tidy                 # remove any newly-unused entries, add any missing ones
make test && make testacc   # confirm nothing broke
```

Do the same for the `tools/` sub-module if it also shows stale indirect deps:

```bash
cd tools && go get -u ./... && go mod tidy && cd ..
```

This is not required before every release -- Go's minimum version selection means stale indirect deps are usually harmless -- but it is worth running every few releases or when the dependency graph looks very stale. Commit the updated `go.mod` and `go.sum` as part of the prepare PR if you do run it.

### Prepare PR

Before tagging a release, open a "Prepare vX.Y.Z" PR that does the following in one commit:

1. **Bump example version pins**: `make bump-examples VERSION=X.Y.Z` -- updates the provider version pin in every runnable example under `examples/`.
2. **Regenerate docs**: `make generate` -- re-renders `docs/index.md` (and all other generated docs) from the updated example template.
3. **Update CHANGELOG.md**: move the `[Unreleased]` section into a new `## [X.Y.Z] - YYYY-MM-DD` section and add a compare link at the bottom.

The GoReleaser workflow includes a pre-release gate (`scripts/check-example-versions.sh`) that verifies every example pin matches the tag being released. Tagging before running `make bump-examples` will fail the gate and prevent any artifacts from being published.
