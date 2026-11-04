// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package extracttool

// TestAcc_ImportRoundtrip_SystemOwnershipTypes reproduces, end to end, the way
// a brownfield user following docs/guides/import-existing.md hits a wall on
// their first attempt.
//
// DataHub bootstraps every instance with built-in __system__ ownership types,
// and datahub_ownership_type refuses to manage them: a plan-time validator
// rejects any type_id beginning with __system__ ("Reserved type_id prefix").
// `terraform plan -generate-config-out` fails the WHOLE run when any single
// resource errors -- it is not a per-resource skip -- so enumerating even one
// system type takes every other resource in the deployment down with it.
//
// The test drives the real pipeline (enumerate -> import.tf -> terraform init
// -> terraform plan -generate-config-out) against the in-memory mock server,
// seeded to look like a real instance: the five built-ins observed on a
// DataHub Cloud v2.3.1 tenant plus one customer-authored type. It asserts the
// customer type is imported and the built-ins are never enumerated.
//
// Prerequisites are the same as TestAcc_ImportRoundtrip_E2E: TF_ACC=1 (the
// opt-in switch, the only thing that skips), ./bin/terraform-provider-datahub
// built, and the terraform CLI on PATH. It is mock-only: seeding __system__
// ownership types into a real instance would write fake built-ins into a
// customer catalog, and the POC tenant this defect was found on is gone.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
	// Blank import for its init(): the real import-target registry, which is
	// what this test exists to exercise.
	_ "github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget/targets"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// systemOwnershipTypeIDs are the built-ins observed on a DataHub Cloud v2.3.1
// instance. __system__agent_owner is in the list on purpose: the provider's
// docs and its own diagnostic name only technical_owner, business_owner,
// data_steward and none, so a fix keyed off that list would miss it.
var systemOwnershipTypeIDs = []string{
	"__system__technical_owner",
	"__system__business_owner",
	"__system__data_steward",
	"__system__agent_owner",
	"__system__none",
}

func TestAcc_ImportRoundtrip_SystemOwnershipTypes(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 to run this acceptance test")
	}

	// Resolve the provider binary and terraform CLI before starting a server,
	// so a missing prerequisite fails rather than half-runs.
	binDir := findProviderBinDir(t)

	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("mock-only: this test seeds __system__ ownership types, which must never be written to a real DataHub instance")
	}

	gmsURL := os.Getenv("DATAHUB_GMS_URL")
	gmsToken := os.Getenv("DATAHUB_GMS_TOKEN")

	devTfrc := writeDevTfrc(t, binDir)
	t.Setenv("TF_CLI_CONFIG_FILE", devTfrc)

	ctx := context.Background()

	client, err := datahub.NewClient(gmsURL, gmsToken)
	if err != nil {
		t.Fatalf("creating DataHub client: %v", err)
	}

	// Seed the built-ins the way DataHub bootstraps them. They are written
	// through the aspect endpoint rather than the resource, because the
	// resource is precisely what refuses them.
	for _, id := range systemOwnershipTypeIDs {
		urn := "urn:li:ownershipType:" + id
		if err := client.WriteOwnershipTypeInfo(ctx, urn, id, "DataHub built-in ownership type"); err != nil {
			t.Fatalf("seeding system ownership type %q: %v", urn, err)
		}
	}

	// One customer-authored type: the thing the user actually wants imported,
	// and the collateral damage when the run aborts.
	const customID = "rt-ownership-type"
	customURN := "urn:li:ownershipType:" + customID
	if err := client.WriteOwnershipTypeInfo(ctx, customURN, "Roundtrip Ownership Type", "customer authored"); err != nil {
		t.Fatalf("seeding customer ownership type %q: %v", customURN, err)
	}
	t.Cleanup(func() {
		_ = client.DeleteOwnershipType(context.Background(), customURN)
	})

	outDir := t.TempDir()
	err = Run(ctx, Options{
		GmsURL:    gmsURL,
		GmsToken:  gmsToken,
		OutputDir: outDir,
		// Only ownership types: this test is about one target, and a narrow
		// run keeps the terraform subprocesses quick.
		Types: "datahub_ownership_type",
		// SkipTerraform: false (default) -- driving the real
		// `terraform plan -generate-config-out` is the point.
	})
	if err != nil {
		t.Fatalf("Run: enumerate-then-import failed for the whole deployment: %v\n"+
			"This is the user-facing failure: a built-in __system__ ownership type was "+
			"enumerated into import.tf, datahub_ownership_type refused it, and "+
			"terraform plan -generate-config-out aborted the entire run.", err)
	}

	requireFiles(t, outDir, "import.tf", "generated.tf", "IMPORT_README.md")

	// import.tf must never name a built-in: that is the enumeration contract.
	importTF := readFile(t, filepath.Join(outDir, "import.tf"))
	if strings.Contains(importTF, "__system__") {
		t.Errorf("import.tf contains an import block for a built-in __system__ ownership type, "+
			"which datahub_ownership_type cannot manage\nfull content:\n%s", importTF)
	}

	// The customer-authored type must survive: a filter that enumerated
	// nothing at all would pass the assertion above for the wrong reason.
	customLabel := LabelFromURN(customURN)
	assertGeneratedTFContains(t, outDir,
		fmt.Sprintf(`resource "datahub_ownership_type" %q`, customLabel),
		fmt.Sprintf("type_id     = %q", customID),
	)
	assertGeneratedTFNotContains(t, outDir, "__system__")
}
