// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// Package extracttool implements the datahub-tf-extract enumerate command.
// It enumerates DataHub resources, generates an import.tf, drives
// terraform plan -generate-config-out, post-processes the generated
// configuration, and writes variables.tf and IMPORT_README.md.
package extracttool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// Options configures the enumerate command.
type Options struct {
	// OutputDir is the directory to write import.tf, generated.tf, variables.tf,
	// and IMPORT_README.md into.
	OutputDir string

	// Types is an optional comma-separated list of resource type names to include
	// (e.g. "datahub_secret,datahub_connection"). Empty means all registered types.
	Types string

	// GmsURL is the DataHub GMS URL. Defaults to DATAHUB_GMS_URL env var.
	GmsURL string

	// GmsToken is the DataHub GMS token. Defaults to DATAHUB_GMS_TOKEN env var.
	GmsToken string

	// ProviderVersion is the CLI version string (from ldflags), used to set the
	// provider version constraint in import.tf. "dev" or empty falls back to the
	// minimum version that includes all features the CLI depends on.
	ProviderVersion string

	// SkipTerraform skips the terraform init/plan steps (useful for tests that
	// only want to assert the import.tf content).
	SkipTerraform bool

	// SkipValidation skips the final terraform plan validation step.
	SkipValidation bool
}

// typeSet parses a comma-separated type filter into a set. Empty means all types.
func typeSet(s string) map[string]bool {
	if s == "" {
		return nil
	}
	m := make(map[string]bool)
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			m[t] = true
		}
	}
	return m
}

// Run executes the full enumerate pipeline.
func Run(ctx context.Context, opts Options) error {
	// Fail fast on a leftover generated.tf from a previous run. `terraform plan
	// -generate-config-out` refuses to overwrite it, so proceeding would
	// silently leave stale, partial config in place. Ask the user to clean up
	// rather than guessing. (Irrelevant under SkipTerraform, which never
	// generates config.)
	if !opts.SkipTerraform {
		genPath := filepath.Join(opts.OutputDir, "generated.tf")
		if _, err := os.Stat(genPath); err == nil {
			return fmt.Errorf("output directory already contains %s from a previous run; "+
				"remove it (or use a fresh --output directory) and re-run", genPath)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("checking for existing generated.tf: %w", err)
		}
	}

	// Resolve credentials.
	gmsURL := opts.GmsURL
	if gmsURL == "" {
		gmsURL = os.Getenv("DATAHUB_GMS_URL")
	}
	gmsToken := opts.GmsToken
	if gmsToken == "" {
		gmsToken = os.Getenv("DATAHUB_GMS_TOKEN")
	}

	client, err := datahub.NewClient(gmsURL, gmsToken)
	if err != nil {
		return fmt.Errorf("creating DataHub client: %w", err)
	}

	// Validate credentials.
	me, err := client.Me(ctx)
	if err != nil {
		return fmt.Errorf("connecting to DataHub at %s: %w", gmsURL, err)
	}
	fmt.Printf("Connected as %s\n\n", me.Urn)

	// Filter targets.
	allowed := typeSet(opts.Types)
	targets := importtarget.All()

	// Enumerate URNs per target.
	var results []enumResult
	totalImports := 0

	fmt.Println("Enumerating:")
	for _, t := range targets {
		if allowed != nil && !allowed[t.ResourceTypeName] {
			continue
		}
		if t.Enumerate == nil {
			fmt.Printf("  %-40s (no enumerator, skipped -- supply URNs manually)\n", t.ResourceTypeName)
			continue
		}

		urns, enumErr := t.Enumerate(ctx, client)
		if enumErr != nil {
			fmt.Printf("  %-40s error: %v\n", t.ResourceTypeName, enumErr)
			continue
		}

		// Dedupe URNs before generating import blocks. A list API that returns
		// the same URN twice (observed with system ingestion sources) would
		// otherwise produce two import {} blocks with distinct resource labels
		// but identical import IDs -- two Terraform resources importing the same
		// DataHub entity, a state conflict on apply.
		urns, dropped := dedupeURNs(urns)
		if dropped > 0 {
			fmt.Printf("  %-40s (dropped %d duplicate URN(s) from enumeration)\n", t.ResourceTypeName, dropped)
		}

		labels := uniqueLabels(urns)
		fmt.Printf("  %-40s %d URNs\n", t.ResourceTypeName, len(urns))
		totalImports += len(urns)

		if len(urns) > 0 {
			results = append(results, enumResult{target: t, urns: urns, labels: labels})
		}
	}
	fmt.Println()

	if totalImports == 0 {
		fmt.Println("No resources found -- nothing to generate.")
		return nil
	}

	// Ensure output directory exists.
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory %s: %w", opts.OutputDir, err)
	}

	// Write import.tf.
	importTFPath := filepath.Join(opts.OutputDir, "import.tf")
	importTFContent := buildImportTF(results, opts.ProviderVersion)
	if err := os.WriteFile(importTFPath, importTFContent, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", importTFPath, err)
	}
	fmt.Printf("Wrote %s (%d import blocks)\n\n", importTFPath, totalImports)

	// Write .gitignore for terraform.tfvars.
	gitignorePath := filepath.Join(opts.OutputDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("terraform.tfvars\n.terraform/\n*.tfstate\n*.tfstate.backup\n"), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", gitignorePath, err)
	}

	if opts.SkipTerraform {
		return nil
	}

	// Run terraform init.
	fmt.Printf("Running: terraform -chdir=%s init ...\n", opts.OutputDir)
	if err := terraformInit(ctx, opts.OutputDir); err != nil {
		return fmt.Errorf("terraform init failed: %w", err)
	}

	// Run terraform plan -generate-config-out.
	fmt.Printf("Running: terraform -chdir=%s plan -generate-config-out=generated.tf ...\n", opts.OutputDir)
	generated, err := terraformGenerateConfig(ctx, opts.OutputDir)
	if err != nil {
		return err
	}
	if !generated {
		return fmt.Errorf("generated.tf was not written -- check terraform output above")
	}

	// Post-process generated.tf.
	generatedPath := filepath.Join(opts.OutputDir, "generated.tf")
	src, err := os.ReadFile(generatedPath)
	if err != nil {
		return fmt.Errorf("reading generated.tf: %w", err)
	}

	fmt.Printf("\nPost-processing %s ...\n", generatedPath)
	rewritten, vars, err := PostProcess(src)
	if err != nil {
		return fmt.Errorf("post-processing generated.tf: %w", err)
	}
	if err := os.WriteFile(generatedPath, rewritten, 0o644); err != nil {
		return fmt.Errorf("writing post-processed generated.tf: %w", err)
	}

	// Write variables.tf.
	if len(vars) > 0 {
		variablesPath := filepath.Join(opts.OutputDir, "variables.tf")
		if err := os.WriteFile(variablesPath, WriteVariablesTF(vars), 0o644); err != nil {
			return fmt.Errorf("writing variables.tf: %w", err)
		}
		fmt.Printf("Wrote %s (%d sensitive variables)\n", variablesPath, len(vars))
	}

	// Write IMPORT_README.md.
	readmePath := filepath.Join(opts.OutputDir, "IMPORT_README.md")
	if err := os.WriteFile(readmePath, WriteImportReadme(totalImports, vars), 0o644); err != nil {
		return fmt.Errorf("writing IMPORT_README.md: %w", err)
	}
	fmt.Printf("Wrote %s\n", readmePath)

	// Final validation plan.
	if !opts.SkipValidation && len(vars) == 0 {
		fmt.Printf("\nRunning: terraform -chdir=%s plan (validation) ...\n", opts.OutputDir)
		if err := terraformPlan(ctx, opts.OutputDir); err != nil {
			return fmt.Errorf("validation plan failed -- the generated config is incomplete or invalid; "+
				"review the terraform output above and do not treat the output as ready to use: %w", err)
		}
	} else if len(vars) > 0 {
		fmt.Printf("\nSkipping final plan: fill in terraform.tfvars first (see IMPORT_README.md).\n")
	}

	// Summary.
	fmt.Printf("\nDone!\n")
	fmt.Printf("  %d resources across %d types\n", totalImports, len(results))
	if len(vars) > 0 {
		fmt.Printf("  %d sensitive variables require values in terraform.tfvars\n", len(vars))
	}
	fmt.Printf("  See %s for next steps\n", readmePath)

	return nil
}

type enumResult struct {
	target importtarget.Target
	urns   []string
	labels []string
}

// minProviderVersion is the lowest provider release that includes
// datahub_ingestion_source ImportState (PR #27) and datahub_connection (PR #26).
// Used as the version floor when the CLI is built without an explicit version
// (e.g. "go run ." or "make install" without a release tag).
const minProviderVersion = "0.3.0"

// dedupeURNs returns urns with duplicates removed, preserving first-seen order,
// and the count of duplicates dropped. DataHub list APIs occasionally return the
// same URN more than once; emitting an import block per occurrence would create
// colliding imports (same import ID, different resource label).
func dedupeURNs(urns []string) ([]string, int) {
	seen := make(map[string]struct{}, len(urns))
	out := urns[:0:0]
	dropped := 0
	for _, u := range urns {
		if _, ok := seen[u]; ok {
			dropped++
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out, dropped
}

// normalizeVersionConstraint turns a raw CLI version string into a value usable
// as the operand of a Terraform ">= X" version constraint. Terraform constraints
// reject a leading "v" and cannot parse git-describe pseudo-versions
// (e.g. "v0.9.0-2-g79369ea" from `make install` off a non-tag commit), so we
// strip the "v" prefix and drop any pre-release/build metadata down to the base
// MAJOR.MINOR.PATCH. Anything that does not reduce to three numeric components
// (including "dev" and "") falls back to minProviderVersion.
func normalizeVersionConstraint(version string) string {
	v := strings.TrimPrefix(version, "v")
	// Drop git-describe suffix ("-N-gHASH"), SemVer pre-release ("-rc.1") and
	// build metadata ("+meta"); keep only the base release version.
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return minProviderVersion
	}
	for _, p := range parts {
		if p == "" {
			return minProviderVersion
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return minProviderVersion
			}
		}
	}
	return v
}

// buildImportTF renders the import.tf content. version is the CLI version string
// from ldflags; "dev", empty, or any non-release pseudo-version triggers the
// minProviderVersion fallback (see normalizeVersionConstraint).
func buildImportTF(results []enumResult, version string) []byte {
	constraint := normalizeVersionConstraint(version)

	var b bytes.Buffer

	b.WriteString("terraform {\n")
	b.WriteString("  required_providers {\n")
	b.WriteString("    datahub = {\n")
	b.WriteString("      source  = \"datahub-project/datahub\"\n")
	fmt.Fprintf(&b, "      version = \">= %s\"\n", constraint)
	b.WriteString("    }\n")
	b.WriteString("  }\n")
	b.WriteString("}\n\n")

	b.WriteString("# Provider reads DATAHUB_GMS_URL and DATAHUB_GMS_TOKEN from the environment.\n")
	b.WriteString("provider \"datahub\" {}\n")

	for _, r := range results {
		b.WriteByte('\n')
		b.WriteString("# --- ")
		b.WriteString(r.target.ResourceTypeName)
		b.WriteString(" ---\n")

		for i, urn := range r.urns {
			label := r.labels[i]
			importID := urn
			if r.target.IDFromURN != nil {
				importID = r.target.IDFromURN(urn)
			}
			fmt.Fprintf(&b, "\nimport {\n")
			fmt.Fprintf(&b, "  to = %s.%s\n", r.target.ResourceTypeName, label)
			fmt.Fprintf(&b, "  id = %q\n", importID)
			fmt.Fprintf(&b, "}\n")
		}
	}

	return b.Bytes()
}
