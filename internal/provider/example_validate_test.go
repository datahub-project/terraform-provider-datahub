// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Running terraform validate over every example is the correctness half of
// TestExampleSnippetCoverage, which only checks that a snippet exists. A snippet
// naming an attribute the provider does not have satisfies the coverage test and
// still misleads every reader who copies it off the registry page.
//
// validate rather than plan, deliberately. Plan additionally reads data sources,
// and every singular data-source snippet looks up an entity by a literal id
// ("alice", "tf-example-pii") that exists on no DataHub instance. Those reads
// fail, so a plan-based check would have to exempt the data-source snippets
// wholesale -- and a data-source snippet is exactly where the last defect of this
// kind was found, during the #105 backfill. validate never reads, so it covers
// the category plan would have to drop.
//
// What validate does exercise: schema conformance (an attribute that does not
// exist, or a required one omitted), HCL and interpolation errors, and the
// provider's ValidateResourceConfig RPC -- which is where schema validators and
// ValidateConfig run, the home of the unknown-value bug class. What it does not
// exercise is ModifyPlan and defaults, and those are already covered for every
// resource by the acceptance suite. What no other test covers is the published
// text itself.

// exampleValidateEnvVar gates these tests the way TF_ACC gates acceptance tests.
// They need a built provider binary and a terraform binary, so they do not run
// under a plain `go test ./...`. `make test-examples` builds the binary and sets
// this.
const exampleValidateEnvVar = "TF_EXAMPLE_VALIDATE"

// exampleExemptions maps a path under examples/ to the reason it cannot be
// validated. Both current entries are properties of the example, not of the
// provider -- neither indicates anything wrong with the snippet as published.
var exampleExemptions = map[string]string{
	"resources/datahub_secret/resource.tf": "reads a caller-supplied credential " +
		"file via file(), which terraform evaluates during validate and which is " +
		"deliberately not committed to this repository",
	"runnable/remote-executor-azure": "declares azurerm and kubernetes alongside " +
		"datahub, and only datahub is dev-overridden, so resolving the others " +
		"would require terraform init and network access",
}

// TestExampleSnippetsValidate runs terraform validate over every registry
// snippet under examples/resources and examples/data-sources.
//
// Snippets are bare resource and data blocks with no terraform or provider
// block, because that is the shape tfplugindocs renders. Each is therefore
// wrapped in the minimum needed to make it a configuration.
func TestExampleSnippetsValidate(t *testing.T) {
	t.Parallel()

	env := setupExampleValidate(t)

	var paths []string
	for _, dir := range []string{"resources", "data-sources"} {
		root := filepath.Join(env.examplesDir, dir)
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(p, ".tf") {
				paths = append(paths, p)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}

	var checked int
	for _, path := range paths {
		rel := mustRel(t, env.examplesDir, path)
		if reason, exempt := exampleExemptions[rel]; exempt {
			t.Logf("%s: skipped, exempt (%s)", rel, reason)
			continue
		}
		checked++

		t.Run(rel, func(t *testing.T) {
			t.Parallel()

			// A snippet is not a configuration on its own. Terraform infers the
			// source of an unqualified provider as hashicorp/<name>, so without
			// required_providers it looks for hashicorp/datahub and never reaches
			// the dev override.
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "wrapper.tf"), `terraform {
  required_providers {
    datahub = {
      source = "datahub-project/datahub"
    }
  }
}

provider "datahub" {}
`)
			copyFile(t, path, filepath.Join(dir, "snippet.tf"))

			if out, err := env.validate(t, dir); err != nil {
				t.Errorf("terraform validate rejected examples/%s:\n\n%s", rel, out)
			}
		})
	}

	// Guard against a vacuous pass, as in TestExampleSnippetCoverage: if the walk
	// ever matches nothing, every assertion above is skipped and the test still
	// reports success.
	const wantAtLeast = 60
	if checked < wantAtLeast {
		t.Errorf("only %d snippets validated, expected at least %d; the walk over "+
			"examples/ is probably not finding them", checked, wantAtLeast)
	}
}

// TestRunnableExamplesValidate runs terraform validate over each complete
// example under examples/runnable.
//
// These already carry their own terraform and provider blocks, so they are
// validated in place rather than wrapped. validate with a dev override performs
// no installation and writes nothing to the directory.
func TestRunnableExamplesValidate(t *testing.T) {
	t.Parallel()

	env := setupExampleValidate(t)

	root := filepath.Join(env.examplesDir, "runnable")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading %s: %v", root, err)
	}

	var checked int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rel := filepath.Join("runnable", e.Name())
		if reason, exempt := exampleExemptions[rel]; exempt {
			t.Logf("%s: skipped, exempt (%s)", rel, reason)
			continue
		}
		checked++

		t.Run(e.Name(), func(t *testing.T) {
			t.Parallel()

			if out, err := env.validate(t, filepath.Join(root, e.Name())); err != nil {
				t.Errorf("terraform validate rejected examples/%s:\n\n%s", rel, out)
			}
		})
	}

	const wantAtLeast = 15
	if checked < wantAtLeast {
		t.Errorf("only %d runnable examples validated, expected at least %d; the "+
			"scan of examples/runnable is probably not finding them", checked, wantAtLeast)
	}
}

// exampleValidateEnv holds the paths a validate run needs.
type exampleValidateEnv struct {
	terraform   string // terraform binary
	cliConfig   string // generated CLI config carrying the dev override
	examplesDir string // absolute path to examples/
}

// setupExampleValidate locates the terraform and provider binaries and writes a
// CLI configuration pointing terraform at the freshly built provider.
//
// The config is generated here rather than reusing the one `make dev-override`
// writes, so the test does not depend on a developer having run that target --
// and, more importantly, so it cannot be silently redirected by the TF_CLI_CONFIG_FILE
// that .mise.env exports into the shell.
func setupExampleValidate(t *testing.T) exampleValidateEnv {
	t.Helper()

	if os.Getenv(exampleValidateEnvVar) == "" {
		t.Skipf("%s not set; run `make test-examples` to validate the examples "+
			"against a built provider binary", exampleValidateEnvVar)
	}

	terraform, err := exec.LookPath("terraform")
	if err != nil {
		t.Fatalf("terraform not found on PATH: %v", err)
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving repository root: %v", err)
	}

	binDir := filepath.Join(root, "bin")
	providerBin := filepath.Join(binDir, "terraform-provider-datahub")
	if _, err := os.Stat(providerBin); err != nil {
		t.Fatalf("provider binary not found at %s: %v\nRun `make install` first, "+
			"or use `make test-examples` which builds it.", providerBin, err)
	}

	// A dev override makes terraform load the provider straight from disk, which
	// means validate needs no `terraform init` and reaches no network at all.
	cliConfig := filepath.Join(t.TempDir(), "dev.tfrc")
	writeFile(t, cliConfig, fmt.Sprintf(`provider_installation {
  dev_overrides {
    "registry.terraform.io/datahub-project/datahub" = %q
  }
  direct {}
}
`, binDir))

	return exampleValidateEnv{
		terraform:   terraform,
		cliConfig:   cliConfig,
		examplesDir: filepath.Join(root, "examples"),
	}
}

// validate runs terraform validate in dir and returns its combined output.
func (e exampleValidateEnv) validate(t *testing.T, dir string) (string, error) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), e.terraform, "validate", "-no-color")
	cmd.Dir = dir
	// An explicit environment rather than the inherited one: DATAHUB_GMS_URL or a
	// stray TF_CLI_CONFIG_FILE in the developer's shell must not change what is
	// being tested. HOME is kept because terraform reads its plugin cache from it.
	cmd.Env = []string{
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
		"TF_CLI_CONFIG_FILE=" + e.cliConfig,
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func mustRel(t *testing.T, base, path string) string {
	t.Helper()
	rel, err := filepath.Rel(base, path)
	if err != nil {
		t.Fatalf("relativising %s against %s: %v", path, base, err)
	}
	return rel
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading %s: %v", src, err)
	}
	writeFile(t, dst, string(b))
}
