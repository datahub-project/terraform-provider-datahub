# Building and developing the provider

## Prerequisites

| Tool | Required | Notes |
|---|---|---|
| Go | yes | Version pinned in `mise.toml` and `go.mod`. Use `mise` to install. |
| Terraform CLI | yes | >= 1.11 required for `WriteOnly` attribute support. |
| `golangci-lint` | no | Only needed for `make lint`. Install via `mise` or see https://golangci-lint.run. |
| `tfplugindocs` | no | Only needed for `make generate`. Managed by the `tools/` sub-module. |

If you use [mise](https://mise.jdx.dev), run `mise install` in the repo root to get the pinned Go version.

## Build the provider

```bash
make install
```

Writes the binary to `./bin/terraform-provider-datahub`.

## Local development with `dev_overrides`

Terraform's `dev_overrides` mechanism lets you use a locally-built binary without publishing a release. The Makefile generates a project-local config file and wires it up via mise.

```bash
make install       # build the binary into ./bin/
make dev-override  # generate dev.tfrc and .mise.env
cd .               # re-trigger mise to set TF_CLI_CONFIG_FILE
```

After the last step, `terraform` in this directory uses your local binary instead of the Registry. To confirm:

```bash
echo $TF_CLI_CONFIG_FILE   # should print the path to dev.tfrc
terraform -chdir=examples/provider-install-verification plan
```

The generated files (`dev.tfrc`, `.mise.env`) are already in `.gitignore`.

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

### Live tests: three-target model

The acceptance tests support three backends, selected at runtime via `DATAHUB_TEST_TARGET`:

| Value | Makefile target | Notes |
|---|---|---|
| unset / `mock` | `make testacc` | Default. In-memory mock server; no DataHub needed. CI gate. |
| `local` | `make testacc-local` | Real DataHub started via `datahub docker quickstart`. Throw-away. |
| `cloud` | `make testacc-cloud` | DataHub cloud tenant built for smoke-testing. Requires `DATAHUB_TEST_ALLOW_CLOUD=1`. |

For live targets the test uses a randomized resource name (`tfprovider-secret-<random>` etc.) so repeated runs and concurrent developers do not collide.

### Live tests against a local DataHub Quickstart

For higher-fidelity coverage run the same scenarios against a real DataHub instance started locally via the `datahub` CLI's Quickstart.

**Prerequisites**

```bash
pip install acryl-datahub        # or 'uv pip install acryl-datahub'
datahub version                  # confirm CLI is on PATH
```

**Start the stack** (takes 5-10 minutes the first time):

```bash
datahub docker quickstart
```

Wait until `http://localhost:9002` loads in a browser.

**Get a personal access token**

1. Open `http://localhost:9002` and log in with `datahub` / `datahub`.
2. Settings -> Access Tokens -> Generate new token.
3. Copy the value.

If you are running an older Quickstart with metadata service authentication disabled, any non-empty string works -- the provider client requires a non-empty token, but DataHub ignores it.

**Export the env vars and run**

```bash
export DATAHUB_GMS_URL=http://localhost:8080
export DATAHUB_GMS_TOKEN=<paste your PAT>
make testacc-local
```

**Verify in the UI**

- Ingestion sources: `http://localhost:9002/ingestion`
- Secrets: Settings -> Secrets

Resources are destroyed at the end of each test step's lifecycle, so a clean run leaves no residue.

**Tear down**

```bash
datahub docker nuke
```

**Caveats**

- If a previous `datahub init` left a stale `~/.datahubenv`, the provider falls back to it when the env vars are empty. Either keep the env vars exported per session or remove `~/.datahubenv`.
- A test that crashes between Create and Destroy can leak resources. Re-running is safe (names are randomized), but stale entities accumulate in DataHub until `datahub docker nuke`.
- Live tests are not run in CI. The mock acceptance tests remain the CI gate.

### Live tests against a DataHub cloud tenant

`make testacc-cloud` runs the same `TestAcc_*` functions against a real DataHub cloud instance. The `DATAHUB_TEST_ALLOW_CLOUD=1` guard is required to prevent accidental use of a production tenant.

```bash
export DATAHUB_GMS_URL=https://your-tenant.acryl.io/api/gms
export DATAHUB_GMS_TOKEN=<cloud PAT>
export DATAHUB_TEST_ALLOW_CLOUD=1
make testacc-cloud
```

Use a tenant set up specifically for smoke-testing. Resources carry the `tfprovider-` prefix so a future sweeper can identify and clean up anything that leaks. Cloud sweeper support is planned before v0.1.

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
