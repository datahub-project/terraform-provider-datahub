// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package importtool

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

// init registers the same import targets as cmd/datahub-tf-import/internal/reg
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
	content := string(buildImportTF(nil))

	const tooPermissive = ">= 0.1.0"
	if strings.Contains(content, tooPermissive) {
		t.Errorf("import.tf contains overly permissive version constraint %q\n"+
			"v0.2.0 on the Terraform Registry pre-dates datahub_ingestion_source "+
			"ImportState (PR #27) and datahub_connection (PR #26); Terraform will "+
			"download it and silently produce a generated.tf with no ingestion "+
			"source or connection resource blocks\n"+
			"fix: pass the CLI version into buildImportTF and emit \">= <version>\"",
			tooPermissive)
	}
}

func ptr[T any](v T) *T { return &v }
