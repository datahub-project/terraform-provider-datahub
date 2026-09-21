// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package extracttool

// TestAcc_ImportRoundtrip_E2E is a full-pipeline acceptance test. It:
//   - Seeds one datahub_ingestion_source and one datahub_secret in DataHub
//     (in-process mock server when DATAHUB_GMS_URL is unset; real DataHub otherwise).
//   - Runs extracttool.Run with SkipTerraform: false, which drives real
//     terraform subprocesses (init, plan -generate-config-out).
//   - Asserts that the generated artefacts have the expected shape:
//     generated.tf contains both resource blocks; the secret's value is
//     replaced by a var reference; variables.tf declares the variable;
//     IMPORT_README.md has strictly-increasing step numbers.
//
// Prerequisites. TF_ACC=1 is the only one that skips; every other missing
// prerequisite fails the test. A skip is indistinguishable from a pass in a
// green suite, so a skipping prerequisite let the entire import path go
// untested without anyone noticing -- which is exactly what happened in CI,
// where the Test job ran with TF_ACC=1 but never built the provider binary.
//   - TF_ACC=1 in the environment (absent: skip -- this is the opt-in switch).
//   - ./bin/terraform-provider-datahub built relative to the module root.
//     Every Makefile target that sets TF_ACC=1 carries an `install`
//     prerequisite, and the CI Test job invokes `make coverage` so that it
//     inherits the same one.
//   - `terraform` CLI on PATH. Pinned in mise.toml, so `mise exec --` provides
//     it; the example suites already treat it as a hard requirement.
//
// providerBinDir below resolves the last two, and is unit-tested by
// TestProviderBinDir -- which needs neither of them present.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

func TestAcc_ImportRoundtrip_E2E(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 to run this acceptance test")
	}

	// Resolve the provider binary and the terraform CLI before anything else,
	// so a missing prerequisite fails before a mock server is started. Both are
	// hard failures on mock and live targets alike.
	binDir := findProviderBinDir(t)

	tg := datahubtesting.SetupTarget(t)
	gmsURL := os.Getenv("DATAHUB_GMS_URL")
	gmsToken := os.Getenv("DATAHUB_GMS_TOKEN")

	// Write a dev.tfrc that routes the datahub provider to our local binary,
	// bypassing the registry. This is the same mechanism as `make dev-override`.
	devTfrc := writeDevTfrc(t, binDir)
	t.Setenv("TF_CLI_CONFIG_FILE", devTfrc)

	ctx := context.Background()

	// Use tg.Name so names are unique on live targets -- tg.Name appends a
	// lowercase random suffix on live (e.g. "rt-source-abc12345") and returns
	// the base unchanged on mock.
	sourceID := tg.Name("rt-source")
	secretName := tg.Name("RT_SECRET")

	client, err := datahub.NewClient(gmsURL, gmsToken)
	if err != nil {
		t.Fatalf("creating DataHub client: %v", err)
	}

	_, err = client.NewDatasourceIngestion(ctx, datahub.DatasourceIngestionInput{
		SourceID:   sourceID,
		SourceName: "Roundtrip Test Source",
		SourceType: "demo-data",
		RecipeJSON: ptr(recipe),
	})
	if err != nil {
		t.Fatalf("seeding ingestion source: %v", err)
	}
	// Ingestion source URN is deterministic from the sourceID.
	ingestionURN := fmt.Sprintf("urn:li:dataHubIngestionSource:%s", sourceID)
	t.Cleanup(func() {
		_ = client.DeleteIngestionSourceByID(context.Background(), sourceID)
	})

	secretURN, err := client.CreateSecret(ctx, datahub.CreateSecretInput{
		Name:  secretName,
		Value: "supersecret",
	})
	if err != nil {
		t.Fatalf("seeding secret: %v", err)
	}
	t.Cleanup(func() {
		_ = client.DeleteSecret(context.Background(), secretURN)
	})

	// On live targets the list APIs (GraphQL/OpenSearch) lag behind writes.
	// Poll until both seeded URNs appear before running the pipeline, otherwise
	// Run may generate a config that omits our seeds.
	if tg.IsLive() {
		waitForURNsInList(ctx, t, client, []string{ingestionURN, secretURN}, 60*time.Second)
	}

	outDir := t.TempDir()
	err = Run(ctx, Options{
		GmsURL:    gmsURL,
		GmsToken:  gmsToken,
		OutputDir: outDir,
		// SkipTerraform: false (default) -- the whole point of this test.
		// SkipValidation is not needed: a non-empty vars list (the secret)
		// naturally skips the final terraform plan validation.
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// --- assertions ---

	requireFiles(t, outDir, "import.tf", "generated.tf", "variables.tf", "IMPORT_README.md")

	// Derive the Terraform labels that the post-processor assigns. On live the
	// URN suffix includes the random component (e.g. "rt-source-abc12345" ->
	// "rt_source_abc12345"), so we compute rather than hard-code them.
	sourceLabel := LabelFromURN(ingestionURN)
	secretLabel := LabelFromURN(secretURN)

	assertGeneratedTFContains(t, outDir,
		fmt.Sprintf(`resource "datahub_ingestion_source" %q`, sourceLabel),
		fmt.Sprintf(`resource "datahub_secret" %q`, secretLabel),
	)
	// Secret value must be replaced by a var reference, not left as null.
	assertGeneratedTFContains(t, outDir, fmt.Sprintf("var.%s_value", secretLabel))
	assertGeneratedTFNotContains(t, outDir, `null # sensitive`)

	// variables.tf must declare the secret variable as sensitive.
	assertVariablesTFContains(t, outDir,
		fmt.Sprintf("variable %q", secretLabel+"_value"),
		`sensitive = true`,
	)

	// README must have strictly-increasing step numbers (guards the lastStep bug).
	assertReadmeStepNumbersIncreasing(t, outDir)
}

// waitForURNsInList polls ListSecretURNs and ListIngestionSourceURNs until all
// of the given URNs appear, or fails the test after timeout. Used on live
// targets to absorb the eventual-consistency lag between entity creation and
// OpenSearch indexing.
func waitForURNsInList(ctx context.Context, t *testing.T, client *datahub.Client, urns []string, timeout time.Duration) {
	t.Helper()
	needed := make(map[string]bool, len(urns))
	for _, u := range urns {
		needed[u] = true
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		secretURNs, _ := client.ListSecretURNs(ctx)
		ingestionURNs, _ := client.ListIngestionSourceURNs(ctx)
		found := 0
		for _, u := range append(secretURNs, ingestionURNs...) {
			if needed[u] {
				found++
			}
		}
		if found == len(needed) {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("timed out after %s waiting for URNs to appear in list APIs: %v", timeout, urns)
}

// findProviderBinDir resolves the provider binary directory, failing the test
// if anything needed to drive real terraform subprocesses is missing. It is a
// thin wrapper over providerBinDir so that the lookup itself stays unit-testable.
func findProviderBinDir(t *testing.T) string {
	t.Helper()

	// During `go test`, cwd is the package directory.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	binDir, err := providerBinDir(cwd)
	if err != nil {
		t.Fatalf("locating provider binary: %v", err)
	}
	return binDir
}

// providerBinDir walks up from startDir to the Go module root (the directory
// containing go.mod), then requires both bin/terraform-provider-datahub and the
// terraform CLI. It returns the directory holding the provider binary.
//
// Every failure is an error rather than a skip, and all three mean the same
// thing to a reader of a green suite: the import pipeline was not exercised at
// all. A module root that cannot be found is a broken invocation rather than an
// environment anyone legitimately runs in; a missing binary is what `make
// install` exists to produce; and terraform is pinned in mise.toml, so its
// absence is a setup error, consistent with how the example suites treat it.
//
// It takes startDir and returns an error rather than reaching for os.Getwd and
// *testing.T so that TestProviderBinDir can point it at a temporary directory.
func providerBinDir(startDir string) (string, error) {
	dir := startDir
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find the Go module root: no go.mod at or above %s", startDir)
		}
		dir = parent
	}

	binDir := filepath.Join(dir, "bin")
	binPath := filepath.Join(binDir, "terraform-provider-datahub")
	if _, err := os.Stat(binPath); err != nil {
		return "", fmt.Errorf("provider binary not found at %s -- run 'make install' first (every Makefile target that sets TF_ACC=1 builds it automatically)", binPath)
	}

	if _, err := exec.LookPath("terraform"); err != nil {
		return "", fmt.Errorf("terraform CLI not found on PATH -- it is pinned in mise.toml, so run via mise (e.g. 'mise exec -- make testacc'): %w", err)
	}

	return binDir, nil
}

// TestProviderBinDir covers the three ways providerBinDir can refuse. It is a
// plain unit test: it needs neither TF_ACC, nor a provider binary, nor
// terraform, because it builds every input it looks at.
//
// The point of the test is the error returns themselves. They used to be
// t.Skip calls, so each of these three cases silently passed the acceptance
// test instead of failing it.
func TestProviderBinDir(t *testing.T) {
	// A directory with a terraform stub on PATH, shared by the cases below
	// that need to get past the CLI check.
	stubPATH := func(t *testing.T) {
		t.Helper()
		pathDir := t.TempDir()
		stub := filepath.Join(pathDir, "terraform")
		if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("writing terraform stub: %v", err)
		}
		t.Setenv("PATH", pathDir)
	}

	writeModuleRoot := func(t *testing.T, withBinary bool) string {
		t.Helper()
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test\n"), 0o644); err != nil {
			t.Fatalf("writing go.mod: %v", err)
		}
		if withBinary {
			binDir := filepath.Join(root, "bin")
			if err := os.MkdirAll(binDir, 0o755); err != nil {
				t.Fatalf("creating bin dir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(binDir, "terraform-provider-datahub"), []byte("stub"), 0o755); err != nil {
				t.Fatalf("writing provider stub: %v", err)
			}
		}
		return root
	}

	t.Run("no module root", func(t *testing.T) {
		stubPATH(t)
		// t.TempDir has no go.mod at or above it, so the walk hits the
		// filesystem root.
		if _, err := providerBinDir(t.TempDir()); err == nil {
			t.Fatal("expected an error when no go.mod exists above the start directory")
		} else if !strings.Contains(err.Error(), "module root") {
			t.Errorf("error should name the module root, got: %v", err)
		}
	})

	t.Run("no provider binary", func(t *testing.T) {
		stubPATH(t)
		root := writeModuleRoot(t, false)
		if _, err := providerBinDir(root); err == nil {
			t.Fatal("expected an error when the provider binary is absent")
		} else if !strings.Contains(err.Error(), "make install") {
			t.Errorf("error should tell the reader to run make install, got: %v", err)
		}
	})

	t.Run("no terraform on PATH", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		root := writeModuleRoot(t, true)
		if _, err := providerBinDir(root); err == nil {
			t.Fatal("expected an error when terraform is not on PATH")
		} else if !strings.Contains(err.Error(), "terraform CLI") {
			t.Errorf("error should name the terraform CLI, got: %v", err)
		}
	})

	t.Run("all prerequisites present", func(t *testing.T) {
		stubPATH(t)
		root := writeModuleRoot(t, true)
		got, err := providerBinDir(root)
		if err != nil {
			t.Fatalf("providerBinDir: %v", err)
		}
		if want := filepath.Join(root, "bin"); got != want {
			t.Errorf("providerBinDir = %q, want %q", got, want)
		}
	})
}

// writeDevTfrc writes a Terraform CLI config file that routes the datahub
// provider to the local binary in binDir, bypassing the Terraform Registry.
// The file is written into a temp dir that t.Cleanup will remove.
func writeDevTfrc(t *testing.T, binDir string) string {
	t.Helper()
	tmpDir := t.TempDir()
	rcPath := filepath.Join(tmpDir, "dev.tfrc")
	content := fmt.Sprintf(`provider_installation {
  dev_overrides {
    "registry.terraform.io/datahub-project/datahub" = %q
  }
  direct {}
}
`, binDir)
	if err := os.WriteFile(rcPath, []byte(content), 0o644); err != nil {
		t.Fatalf("writing dev.tfrc: %v", err)
	}
	return rcPath
}

// requireFiles fails the test if any of the named files are missing from dir.
func requireFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected file %s to exist: %v", p, err)
		}
	}
}

// assertGeneratedTFContains fails if generated.tf does not contain all of
// the given substrings.
func assertGeneratedTFContains(t *testing.T, dir string, substrings ...string) {
	t.Helper()
	content := readFile(t, filepath.Join(dir, "generated.tf"))
	for _, s := range substrings {
		if !strings.Contains(content, s) {
			t.Errorf("generated.tf missing expected substring %q\nfull content:\n%s", s, content)
		}
	}
}

// assertGeneratedTFNotContains fails if generated.tf contains any of the
// given substrings.
func assertGeneratedTFNotContains(t *testing.T, dir string, substrings ...string) {
	t.Helper()
	content := readFile(t, filepath.Join(dir, "generated.tf"))
	for _, s := range substrings {
		if strings.Contains(content, s) {
			t.Errorf("generated.tf must not contain %q (post-processor should have replaced it)\nfull content:\n%s", s, content)
		}
	}
}

// assertVariablesTFContains fails if variables.tf does not contain all of
// the given substrings.
func assertVariablesTFContains(t *testing.T, dir string, substrings ...string) {
	t.Helper()
	content := readFile(t, filepath.Join(dir, "variables.tf"))
	for _, s := range substrings {
		if !strings.Contains(content, s) {
			t.Errorf("variables.tf missing expected substring %q\nfull content:\n%s", s, content)
		}
	}
}

// assertReadmeStepNumbersIncreasing parses the IMPORT_README.md and verifies
// that the ## Step N -- ... headings have strictly increasing N values.
// This guards against the bug where two headings were both labelled "Step 2".
func assertReadmeStepNumbersIncreasing(t *testing.T, dir string) {
	t.Helper()
	content := readFile(t, filepath.Join(dir, "IMPORT_README.md"))

	re := regexp.MustCompile(`(?m)^## Step (\d+) --`)
	matches := re.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		t.Error("IMPORT_README.md has no '## Step N --' headings")
		return
	}

	prev := 0
	for _, m := range matches {
		n, _ := strconv.Atoi(m[1])
		if n <= prev {
			t.Errorf("IMPORT_README.md step numbers not strictly increasing: got %d after %d\nfull content:\n%s",
				n, prev, content)
			return
		}
		prev = n
	}
}

// readFile reads a file and fails the test if it cannot be read.
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}
