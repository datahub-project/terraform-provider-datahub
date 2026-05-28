// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package exporttool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// recipe is a minimal ingestion source recipe used for seeding test data.
const recipe = `{"source":{"type":"demo-data","config":{}},"sink":{"type":"datahub-rest","config":{}}}`

// init registers the same import targets as cmd/datahub-tf-export/internal/reg
// (which cannot be imported here due to Go's internal package rule). The order
// mirrors reg.go: secrets last, so that any Required+WriteOnly error from
// terraform plan -generate-config-out does not abort generation of earlier types.
//
// The connection filter is co-registered here so changes to the real filter are
// caught by TestRunImportTF_importBlocksPerType.
func init() {
	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_ingestion_source",
		DataSourceTypeName: "datahub_ingestion_sources",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			return c.ListIngestionSourceURNs(ctx)
		},
		IDFromURN: func(urn string) string {
			return strings.TrimPrefix(urn, "urn:li:dataHubIngestionSource:")
		},
		OSSCompatible: true,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_connection",
		DataSourceTypeName: "datahub_connections",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			all, err := c.ListConnectionURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing connection URNs: %w", err)
			}
			const prefix = "urn:li:dataHubConnection:"
			var filtered []string
			for _, urn := range all {
				id := strings.TrimPrefix(urn, prefix)
				if strings.HasPrefix(id, "urn_li_") || strings.HasPrefix(id, "__") {
					continue
				}
				filtered = append(filtered, urn)
			}
			return filtered, nil
		},
		IDFromURN: func(urn string) string {
			return strings.TrimPrefix(urn, "urn:li:dataHubConnection:")
		},
		OSSCompatible: true,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName: "datahub_remote_executor_pool",
		IDFromURN: func(urn string) string {
			return strings.TrimPrefix(urn, "urn:li:dataHubRemoteExecutorPool:")
		},
		OSSCompatible: false,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_secret",
		DataSourceTypeName: "datahub_secrets",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			return c.ListSecretURNs(ctx)
		},
		IDFromURN:     func(urn string) string { return urn },
		OSSCompatible: true,
	})
}

// TestRunImportTF_importBlocksPerType verifies that Run with SkipTerraform writes
// import.tf with the correct number of import blocks per resource type, and that
// system/OAuth connections (which the datahub_connection resource cannot import)
// are excluded. This guards against silent drops where a resource type produces
// zero import blocks despite having matching entities in DataHub.
func TestRunImportTF_importBlocksPerType(t *testing.T) {
	srv := datahubtesting.NewServer(t)
	client, err := datahub.NewClient(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("creating DataHub client: %v", err)
	}
	ctx := context.Background()

	// Seed 2 ingestion sources.
	for _, id := range []string{"source-alpha", "source-beta"} {
		_, err := client.NewDatasourceIngestion(ctx, datahub.DatasourceIngestionInput{
			SourceID:   id,
			SourceName: id,
			SourceType: "demo-data",
			RecipeJSON: ptr(recipe),
		})
		if err != nil {
			t.Fatalf("seeding ingestion source %q: %v", id, err)
		}
	}

	// Seed 1 user-managed connection (UUID-format ID -- importable).
	_, err = client.UpsertConnection(ctx, datahub.UpsertConnectionInput{
		ID:       "4c7cf6d3-5720-443c-bdbf-febd5c7644a8",
		Name:     "prod-databricks",
		Platform: "databricks",
		Blob:     `{}`,
	})
	if err != nil {
		t.Fatalf("seeding user connection: %v", err)
	}

	// Seed 1 system connection (ID prefix "__") -- must be filtered out.
	_, err = client.UpsertConnection(ctx, datahub.UpsertConnectionInput{
		ID:       "__system_teams-0",
		Name:     "system-teams",
		Platform: "teams",
		Blob:     `{}`,
	})
	if err != nil {
		t.Fatalf("seeding system connection: %v", err)
	}

	// Seed 1 OAuth connection (ID prefix "urn_li_") -- must be filtered out.
	_, err = client.UpsertConnection(ctx, datahub.UpsertConnectionInput{
		ID:       "urn_li_corpuser_alice_example_com__urn_li_service_abc123",
		Name:     "oauth-connection",
		Platform: "oauth",
		Blob:     `{}`,
	})
	if err != nil {
		t.Fatalf("seeding OAuth connection: %v", err)
	}

	// Seed 1 secret.
	_, err = client.CreateSecret(ctx, datahub.CreateSecretInput{
		Name:  "MY_SECRET",
		Value: "supersecret",
	})
	if err != nil {
		t.Fatalf("seeding secret: %v", err)
	}

	outDir := t.TempDir()
	opts := Options{
		GmsURL:        srv.URL,
		GmsToken:      "test-token",
		OutputDir:     outDir,
		SkipTerraform: true,
	}
	if err := Run(ctx, opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(outDir, "import.tf"))
	if err != nil {
		t.Fatalf("reading import.tf: %v", err)
	}
	content := string(raw)

	cases := []struct {
		resourceType string
		want         int
	}{
		// 2 ingestion sources seeded.
		{"datahub_ingestion_source", 2},
		// 1 user connection; 2 system/OAuth connections must be filtered out.
		{"datahub_connection", 1},
		// 1 secret seeded.
		{"datahub_secret", 1},
		// remote_executor_pool has no Enumerate, so no import blocks.
		{"datahub_remote_executor_pool", 0},
	}
	for _, c := range cases {
		got := strings.Count(content, "to = "+c.resourceType+".")
		if got != c.want {
			t.Errorf("import blocks for %s: got %d, want %d\nimport.tf:\n%s",
				c.resourceType, got, c.want, content)
		}
	}
}

// TestBuildImportTF_VersionConstraint guards against the class of failure where
// import.tf's provider version constraint is so permissive that Terraform
// downloads an old registry release that pre-dates import support for the
// resource types the CLI relies on.
//
// Concretely: v0.2.0 on the registry was published before datahub_ingestion_source
// ImportState (PR #27) and datahub_connection (PR #26) were added. With the
// current ">= 0.1.0" floor, Terraform downloads v0.2.0 and all 75 ingestion
// source imports fail with "Resource Import Not Implemented", producing a
// generated.tf that contains only secrets.
//
// The fix is to pass the CLI version into buildImportTF and emit
// version = ">= <cli_version>", ensuring users always get a provider release
// that was tested with the same feature set as the CLI binary they downloaded.
func TestBuildImportTF_VersionConstraint(t *testing.T) {
	cases := []struct {
		name        string
		version     string
		wantContain string
	}{
		// "dev" builds (make install without a release tag) must fall back to the
		// minimum provider release that includes datahub_ingestion_source ImportState
		// (PR #27) and datahub_connection (PR #26), not the old ">= 0.1.0" floor
		// that lets Terraform download v0.2.0 and silently produce an empty generated.tf.
		{"dev fallback", "dev", ">= " + minProviderVersion},
		{"empty fallback", "", ">= " + minProviderVersion},
		// Release builds must pin to their own version so users always get a
		// provider release tested alongside the CLI binary they downloaded.
		{"explicit version", "0.4.0", ">= 0.4.0"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			content := string(buildImportTF(nil, c.version))

			const tooPermissive = ">= 0.1.0"
			if strings.Contains(content, tooPermissive) {
				t.Errorf("import.tf contains overly permissive version constraint %q; "+
					"v0.2.0 on the Terraform Registry pre-dates datahub_ingestion_source "+
					"ImportState (PR #27) and datahub_connection (PR #26), so Terraform "+
					"downloads it and produces a generated.tf with no ingestion source or "+
					"connection resource blocks", tooPermissive)
			}
			if !strings.Contains(content, c.wantContain) {
				t.Errorf("import.tf missing expected constraint %q\ngot:\n%s", c.wantContain, content)
			}
		})
	}
}

func TestWriteVariablesTF(t *testing.T) {
	t.Run("empty returns nil", func(t *testing.T) {
		if got := WriteVariablesTF(nil); got != nil {
			t.Errorf("expected nil for empty vars, got %q", got)
		}
	})

	t.Run("single var", func(t *testing.T) {
		vars := []Variable{{
			Name:          "datahub_secret_my_secret_value",
			Attr:          "value",
			ResourceLabel: "my_secret",
			ResourceType:  "datahub_secret",
		}}
		got := string(WriteVariablesTF(vars))
		if !strings.Contains(got, `variable "datahub_secret_my_secret_value"`) {
			t.Errorf("missing variable declaration; got:\n%s", got)
		}
		if !strings.Contains(got, "sensitive = true") {
			t.Errorf("missing sensitive = true; got:\n%s", got)
		}
		if !strings.Contains(got, "datahub_secret.my_secret.value") {
			t.Errorf("missing description; got:\n%s", got)
		}
	})

	t.Run("multiple vars have blank line separator", func(t *testing.T) {
		vars := []Variable{
			{Name: "var_a", Attr: "a", ResourceLabel: "r", ResourceType: "t"},
			{Name: "var_b", Attr: "b", ResourceLabel: "r", ResourceType: "t"},
		}
		got := string(WriteVariablesTF(vars))
		if !strings.Contains(got, "}\n\nvariable") {
			t.Errorf("expected blank line between variable blocks; got:\n%s", got)
		}
	})
}

func TestWriteImportReadme(t *testing.T) {
	t.Run("no vars -- step numbering starts at 1", func(t *testing.T) {
		got := string(WriteImportReadme(5, nil))
		if !strings.Contains(got, "5 existing DataHub resources") {
			t.Errorf("missing import count; got:\n%s", got)
		}
		if strings.Contains(got, "terraform.tfvars") {
			t.Errorf("should not mention terraform.tfvars when no vars; got:\n%s", got)
		}
		if !strings.Contains(got, "## Step 2 -- Verify") {
			t.Errorf("expected Step 2 to be Verify with no vars; got:\n%s", got)
		}
		if !strings.Contains(got, "## Step 3 -- Apply") {
			t.Errorf("expected Step 3 to be Apply with no vars; got:\n%s", got)
		}
	})

	t.Run("with vars -- step numbering shifts", func(t *testing.T) {
		vars := []Variable{{Name: "datahub_secret_x_value"}}
		got := string(WriteImportReadme(3, vars))
		if !strings.Contains(got, "terraform.tfvars") {
			t.Errorf("expected terraform.tfvars mention when vars present; got:\n%s", got)
		}
		if !strings.Contains(got, "datahub_secret_x_value") {
			t.Errorf("expected variable name in tfvars stub; got:\n%s", got)
		}
		if !strings.Contains(got, "## Step 3 -- Verify") {
			t.Errorf("expected Step 3 to be Verify with vars; got:\n%s", got)
		}
		if !strings.Contains(got, "## Step 4 -- Apply") {
			t.Errorf("expected Step 4 to be Apply with vars; got:\n%s", got)
		}
	})
}

func TestTypeSet(t *testing.T) {
	t.Run("empty returns nil", func(t *testing.T) {
		if typeSet("") != nil {
			t.Error("expected nil for empty string")
		}
	})

	t.Run("single type", func(t *testing.T) {
		m := typeSet("datahub_secret")
		if !m["datahub_secret"] {
			t.Errorf("expected datahub_secret in set; got %v", m)
		}
		if len(m) != 1 {
			t.Errorf("expected 1 entry, got %d", len(m))
		}
	})

	t.Run("multiple types with spaces", func(t *testing.T) {
		m := typeSet("datahub_secret, datahub_connection")
		if !m["datahub_secret"] || !m["datahub_connection"] {
			t.Errorf("expected both types in set; got %v", m)
		}
		if len(m) != 2 {
			t.Errorf("expected 2 entries, got %d", len(m))
		}
	})
}

func ptr[T any](v T) *T { return &v }
