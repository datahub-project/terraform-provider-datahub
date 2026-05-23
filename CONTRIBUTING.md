# Contributing to terraform-provider-datahub

Thank you for your interest in contributing. This document covers how to report issues, propose changes, and get your pull request merged.

## Code of conduct

Be respectful and constructive. We follow the same norms as the broader DataHub community.

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
