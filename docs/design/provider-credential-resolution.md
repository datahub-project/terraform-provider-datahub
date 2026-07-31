# Credential resolution, and why the CLI-config fallback was removed

Maintainer-facing. Records a decision that previously had no written trace at all, so that the removed behaviour is not helpfully reinstated by someone who never saw why it went.

## Current behaviour

The provider resolves `gms_url` and `gms_token` from exactly two places, in order:

1. `gms_url` / `gms_token` in the provider configuration.
2. `DATAHUB_GMS_URL` / `DATAHUB_GMS_TOKEN` in the environment.

There is no third tier. If neither supplies a value, `Configure` fails with a diagnostic that explains the removal (`removedCLIConfigFallbackHint` in `internal/provider/provider.go`).

## What was removed

Until 0.18.0 there was a third tier: when the configuration and the environment were both empty, the provider read `gms.server` and `gms.token` from `~/.datahubenv`, the DataHub CLI's configuration file. This was implemented directly in Go - `os.ReadFile` plus a `yaml.Unmarshal` into a two-field struct - not delegated to the CLI or to any Python component.

It was present in the repository's initial commit, whose message is `Initial commit.`, and no design document, README, or CONTRIBUTING section ever explained it. The only traces were the generated schema descriptions and one CHANGELOG line, both describing the behaviour rather than justifying it.

The most likely original motivation, reconstructed rather than recorded: it gave `terraform plan` the same zero-configuration feel as `datahub` commands for an operator who had already run `datahub init`.

## Why it was removed

**It broke reproducibility, which is the property Terraform exists to provide.** Two engineers running the same configuration against the same state could target different DataHub instances, depending on what each machine's CLI had last been pointed at. The configuration no longer determined the outcome.

That is also why the CLI is right to work this way and the provider was not. `datahub` is imperative - a human at a keyboard, where ambient context is convenient and its scope is one command. Terraform is declarative and reproducible. The same mechanism is appropriate in one and a category error in the other, so "the CLI does it" was never a sufficient reason for the provider to.

**The failure mode was silent and severe.** An empty `provider "datahub" {}` block is not inert: it targeted whatever the CLI last used, which could be production, with nothing in the configuration to say so. A newcomer evaluating the provider had no way to anticipate that.

**Ambient credential files are a pattern being retired, not a stable norm.** The obvious counter-argument is that the AWS, Google and Kubernetes providers all read ambient credentials by default. Two things answer it. Those mechanisms are the primary, expected, universally documented path, with trained operator reflexes around them (`aws sts get-caller-identity`) and, increasingly, brokered short-lived credentials via SSO replacing long-lived files. `~/.datahubenv` has none of that: it is not documented as a Terraform mechanism, nobody expects it, and it holds a long-lived bearer token in plaintext. The adjacent lesson is the Kubernetes provider's ambient kubeconfig, a well-known footgun its own documentation now steers people away from for anything that matters.

**DataHub has no brokered short-lived credential path to migrate to.** API access is via personal access tokens or service-account tokens; SSO/OIDC covers UI login, not API auth. So the credential layer cannot be expected to get safer underneath the provider, which puts more of the burden on the configuration layer being explicit, not less.

**Tiers 1 and 2 already support the safer pattern; tier 3 structurally cannot.** `gms_token = data.external.token.result.token`, or a wrapper exporting `DATAHUB_GMS_TOKEN` from a one-shot mint, both work today and both accommodate a short-lived token. Tier 3 could only ever read a file written earlier - it *is* the long-lived-credential-on-disk pattern, with no variant that is compatible with brokered auth. Removing it costs nothing the other two tiers do not already do better.

## Options considered and rejected

**Warn when the fallback is used.** Cheap and non-breaking, and it would have made the discovery below immediate. Rejected as a sufficient response: Terraform warnings scroll past, especially in CI, and at 0.x with a small known user base a deprecation cycle would have produced no telemetry and no reports - a release spent learning nothing before removing it anyway.

**Make it opt-in** (`use_cli_config = true`). Rejected on two grounds. An opt-in flag institutionalises the behaviour: it adds schema surface, documentation, tests and a compatibility promise for something judged wrong in principle, and opt-in settings get copied from blog posts by people who have not read what they do. More decisively, no use case survives scrutiny - CI has no `~/.datahubenv`, shared team use should not want machine-dependent targeting, and the solo-laptop case is served equally by one `export` in a shell profile.

## How it was found

A `terraform plan`-only demonstration of an unrelated bug was described as needing no credentials, which is true of `plan`. The operator then deliberately cleared every credential they knew of from the shell and ran `apply` expecting it to fail for lack of authentication. It failed instead at the DataHub API, having authenticated from `~/.datahubenv` against a shared instance and been rejected only because the demo's dataset URN was fabricated.

Worth recording because of the shape: the person hit this *while actively trying to run without credentials*, on a machine whose owner maintains an explicit prod-credential guard. That guard inspects environment variables, so a file-based ambient credential is invisible to it - "the operator will have controls" is not an available mitigation for this class.

## Guards

Two tests in `internal/provider/provider_credentials_test.go`:

- `TestNoDatahubEnvReferenceInCredentialResolution` fails if any non-test file in the provider package contains the Go string literal `".datahubenv"`. Structural rather than behavioural on purpose: proving a file is *not* read would mean controlling `HOME` for the whole test process, whereas asserting the code contains no such read is both stronger and cheaper. Confirmed to fail when a read is reintroduced.
- `TestRemovedCLIConfigFallbackHintIsActionable` pins the upgrade message's content. That diagnostic is the only place an affected user learns what happened, so it must keep naming the file, the version, the replacement environment variables, and the reason.

## Upgrade impact

Breaking for any configuration that supplied neither the attributes nor the environment variables and relied on `~/.datahubenv`. Such a configuration now fails at `Configure` with the explanation above.

None of the repository's own examples or tests were affected: all 18 runnable example READMEs already document `DATAHUB_GMS_URL`/`DATAHUB_GMS_TOKEN`, and the acceptance harness (`datahubtesting.SetupTarget`) reads only environment variables. The one remaining `~/.datahubenv` reference in an example README is legitimate - `assertion-volume-sqlite` genuinely invokes the `datahub` CLI, which needs its own configuration, separately from how the provider authenticates.

Removing the fallback also dropped `gopkg.in/yaml.v3` as a direct dependency.
