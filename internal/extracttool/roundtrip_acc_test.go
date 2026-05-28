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
// Prerequisites (test skips with a clear message if absent):
//   - TF_ACC=1 in the environment.
//   - ./bin/terraform-provider-datahub built relative to the module root.
//     Run `make install` first. `make testacc-local` and `make testacc-quickstart`
//     build the binary automatically; `make testacc` (mock-only) does not.
//     A missing binary fails the test on live targets and skips it on mock.
//   - `terraform` CLI on PATH.

import (
	"context"
	"fmt"
	"os"
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

	// Choose mock or live target first -- we need tg.IsLive() to decide
	// whether a missing binary is a hard failure or a graceful skip.
	tg := datahubtesting.SetupTarget(t)

	// Locate the provider binary (built by `make install`).
	// On live targets the Makefile install prereq guarantees it is present, so
	// a missing binary is a build failure -- fail fast. On mock targets the
	// binary is not guaranteed (make testacc deliberately skips the build step),
	// so a graceful skip is appropriate.
	binDir := findProviderBinDir(t, tg.IsLive())
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

// findProviderBinDir walks up from the current working directory to find the
// Go module root (the directory containing go.mod), then checks for
// bin/terraform-provider-datahub. When requireBinary is true (live targets),
// a missing binary is a hard failure; otherwise the test is skipped.
func findProviderBinDir(t *testing.T, requireBinary bool) string {
	t.Helper()

	// During `go test`, cwd is the package directory.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("could not find module root (go.mod) -- skipping")
			return ""
		}
		dir = parent
	}

	binPath := filepath.Join(dir, "bin", "terraform-provider-datahub")
	if _, err := os.Stat(binPath); err != nil {
		msg := fmt.Sprintf("provider binary not found at %s -- run 'make install' first (make testacc-local and make testacc-quickstart do this automatically)", binPath)
		if requireBinary {
			t.Fatal(msg)
		} else {
			t.Skip(msg)
		}
		return ""
	}

	_, err = lookupTerraform()
	if err != nil {
		t.Skip("terraform CLI not found on PATH -- install terraform to run this test")
		return ""
	}

	return filepath.Join(dir, "bin")
}

// lookupTerraform reports whether the terraform CLI is on PATH.
func lookupTerraform() (string, error) {
	// exec.LookPath is in os/exec but we avoid importing it just for this;
	// replicate the check via PATH iteration.
	pathEnv := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(pathEnv) {
		candidate := filepath.Join(dir, "terraform")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("terraform not found on PATH")
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
