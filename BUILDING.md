# Building and developing the provider

## Prerequisites

| Tool | Required | Notes |
|---|---|---|
| Go | yes | Version pinned in `mise.toml` and `go.mod`. Use `mise` to install. |
| Terraform CLI | yes | >= 1.11 required for `WriteOnly` attribute support. |
| `golangci-lint` | no | Only needed for `make lint`. Install via `mise` or see https://golangci-lint.run. |
| `tfplugindocs` | no | Only needed for `make generate`. Managed by the `tools/` sub-module. |
| Docker | no | Required for `make testacc-quickstart` only. |
| `jq` | no | Required for `make testacc-local` and `make testacc-quickstart` (PAT minting). Installed automatically by `mise install`. |

If you use [mise](https://mise.jdx.dev), run `mise install` in the repo root to get all pinned tools (Go, Terraform, golangci-lint, Python, uv).

## First-time setup

Terraform's `dev_overrides` mechanism lets you use a locally-built binary without publishing a release. The Makefile generates a project-local config file (`dev.tfrc`) and wires it up via mise.

```bash
mise install       # install all pinned tools and create the .venv
make install       # build the provider binary into ./bin/
make dev-override  # generate dev.tfrc and .mise.env; install the datahub CLI
cd .               # re-trigger mise to activate TF_CLI_CONFIG_FILE and .venv
```

After that, `terraform` in this directory uses your local binary instead of the Registry, and `datahub` is available from the project venv without any global install. To confirm:

```bash
echo $TF_CLI_CONFIG_FILE   # should print the path to dev.tfrc
terraform -chdir=examples/provider-install-verification plan
```

The generated files (`dev.tfrc`, `.mise.env`) are already in `.gitignore`.

## Build the provider

```bash
make install
```

Writes the binary to `./bin/terraform-provider-datahub`. Re-run this after any code change; `terraform` picks up the new binary immediately.

## Running tests

### Unit and HTTP-client tests (no Terraform required)

```bash
make test
```

Runs all tests in `pkg/tools/uid/` (pure unit) and `pkg/datahub/` (HTTP client against `httptest.Server`). Completes in under a second.

### Full acceptance tests (mock-backed, no live DataHub)

```bash
make testacc
```

Sets `TF_ACC=1` and runs the Plugin Framework acceptance tests in `internal/provider/`. Each test spins up an in-memory mock server (`datahubtesting.NewServer`) -- no network access or DataHub instance needed. Completes in a few seconds.

The `TestAcc_Secret_Lifecycle` test requires Terraform CLI >= 1.11 and is automatically skipped if an older CLI is found.

### Live tests: target overview

Each Makefile target enforces WHERE and HOW tests run. The target name is the source of truth for location and launch method; DATAHUB_CLOUD=1 is a separate caller-supplied signal about what capabilities to expect.

| Target | Where | How | Without `DATAHUB_CLOUD=1` | With `DATAHUB_CLOUD=1` |
|---|---|---|---|---|
| `make testacc` | nowhere (no network) | in-memory mock | All tests (mock simulates Cloud) | n/a - mock always runs all |
| `make testacc-local` | `localhost:8080` | BYO instance | OSS error-path tests | Cloud lifecycle tests |
| `make testacc-quickstart` | `localhost:8080` | boots fresh OSS Quickstart | OSS error-path tests | Cloud lifecycle tests |
| `make testacc-remote` | anywhere (`DATAHUB_GMS_URL`) | BYO remote instance | OSS error-path tests | Cloud lifecycle tests |

`testacc-local` and `testacc-quickstart` always hard-code `localhost:8080`; any `DATAHUB_GMS_URL` in the shell environment is ignored. `testacc-remote` requires `DATAHUB_GMS_URL` and refuses loopback URLs.

**Which tests run where:**

- *Cloud-only tests* (e.g. `TestAcc_RemoteExecutorPool_Lifecycle`) use `tg.RequireCloud(t)`. They always run against the mock (which simulates Cloud). Against any live target they run only when `DATAHUB_CLOUD=1` is set; otherwise they are skipped.
- *OSS-error-path tests* (e.g. `TestAcc_RemoteExecutorPool_OSS_RejectsWithCloudOnlyError`) use `tg.RequireOSS(t)`. They are skipped on the mock (which simulates Cloud) and skipped on any live target when `DATAHUB_CLOUD=1` is set. They run on live targets when `DATAHUB_CLOUD` is unset.

For live targets the test uses a randomized resource name (`tfprovider-secret-<random>` etc.) so repeated runs and concurrent developers do not collide.

### Live tests against a local DataHub Quickstart

`make testacc-quickstart` runs the full live acceptance suite against a throw-away DataHub instance with zero manual steps. It requires Docker and the `datahub` CLI (installed by `make dev-override`).

```bash
make testacc-quickstart
```

This target:
1. Checks if a Quickstart is already healthy and reuses it; otherwise calls `datahub docker quickstart` (first pull takes 5-10 min).
2. Runs `make testacc-local`, which mints a fresh PAT against `http://localhost:8080` and runs tests.
3. Always calls `datahub docker nuke` on exit, whether tests pass or fail.

**Knobs**

| Variable | Default | Effect |
|---|---|---|
| `FRESH=1` | off | Nuke any existing Quickstart before booting a fresh one. |
| `KEEP_QUICKSTART=1` | off | Skip the automatic nuke on exit (for post-mortem inspection). |

```bash
# Always start fresh:
FRESH=1 make testacc-quickstart

# Keep containers running after tests for inspection:
KEEP_QUICKSTART=1 make testacc-quickstart
# ... inspect logs, UI at http://localhost:9002 ...
make quickstart-down
```

**Verify in the UI** (while stack is running)

- Ingestion sources: `http://localhost:9002/ingestion`
- Secrets: Settings -> Secrets

**Caveats**

- `testacc-local` and `testacc-quickstart` always hit `http://localhost:8080`. Any `DATAHUB_GMS_URL` or `DATAHUB_GMS_TOKEN` in your shell is ignored; the Makefile mints a fresh PAT for each run.
- If a previous `datahub init` left a stale `~/.datahubenv`, the provider falls back to it when the env vars are empty. Either keep the env vars exported per session or remove `~/.datahubenv`.
- A test that crashes between Create and Destroy can leak resources. Re-running is safe (names are randomized), but stale entities accumulate in DataHub until `datahub docker nuke`.
- Live tests run in CI on release tag pushes (as a gate before GoReleaser), on a nightly schedule, and on PRs labeled `run-live-ci`.

### Live tests against a remote tenant

`make testacc-remote` runs `TestAcc_*` functions against any non-loopback DataHub instance. Set `DATAHUB_GMS_URL` and `DATAHUB_GMS_TOKEN` in the shell before invoking it.

```bash
export DATAHUB_GMS_URL=https://your-staging-instance.example.com/api/gms
export DATAHUB_GMS_TOKEN=<PAT>
make testacc-remote
```

`DATAHUB_CLOUD=1` toggles which half of the test suite runs. Without it, OSS error-path tests verify that Cloud-only resources fail cleanly on non-Cloud instances. With it, Cloud lifecycle tests verify full CRUD against a real Cloud tenant:

```bash
export DATAHUB_GMS_URL=https://your-tenant.acryl.io/gms
export DATAHUB_GMS_TOKEN=<PAT>
DATAHUB_CLOUD=1 make testacc-remote
```

Use a tenant set up specifically for smoke-testing. Resources carry the `tfprovider-` prefix so a future sweeper can identify and clean up anything that leaks.

### Inspect-then-destroy workflow

`resource.Test` always destroys resources at the end of the last test step. To deploy a resource, inspect it in the UI, and then manually clean it up, use the example directories directly:

```bash
cd examples/resources/datahub_secret
export DATAHUB_GMS_URL=http://localhost:8080
export DATAHUB_GMS_TOKEN=<PAT>
terraform init
terraform apply              # creates the resource; leaves it in place
# inspect in DataHub UI at http://localhost:9002/settings/secrets
terraform destroy            # tear it down when ready
```

This works against the local Quickstart or any cloud tenant without any changes to the provider.

## Coverage reports

The `make test` / `make testacc` targets print per-package coverage using Go's default mode, which does not track cross-package calls. Use the dedicated coverage targets to get a merged project-wide figure.

```bash
make coverage
```

Runs all tests (with `TF_ACC=1`) under `-coverpkg=./internal/...`, which instruments every internal package regardless of which test package calls into it. Prints the merged `total:` line at the end and writes `coverage.out`.

For a per-line HTML view:

```bash
make coverage-html
open coverage.html
```

Green = covered, red = not covered. The HTML includes `internal/provider/datahubtesting/` so you can see how much of the mock server is exercised by the acceptance tests.

**What is excluded:** `main.go` (framework entry-point wiring) and the `tools/` subdirectory (a separate Go module for doc generation) are naturally outside the `./internal/...` scope and do not appear in the report.

Both `coverage.out` and `coverage.html` are in `.gitignore`.

## Linting

```bash
make lint
```

Runs `golangci-lint run`. Requires `golangci-lint` to be installed.

## Generating registry docs

```bash
make generate
```

Runs `go generate ./...` inside the `tools/` sub-module, which invokes `tfplugindocs` to regenerate the Markdown under `docs/`. Commit the generated output.

## License headers

New source files use the Apache-2.0 header:

```go
// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0
```

Files derived from the HashiCorp scaffolding template are MPL-2.0. Do not strip the HashiCorp copyright notice from those files (see `NOTICE` for the list). Each file's `SPDX-License-Identifier` comment is authoritative.
