// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider"
)

// snippetExemptions maps a resource or data source type name that intentionally
// ships without a registry example to a human-readable reason. Add an entry here
// when a type genuinely cannot carry one, rather than silently omitting it.
//
// Empty, and worth keeping that way. Twenty-two types were missing a snippet
// before the backfill, and the reason none of it was noticed is that a missing
// snippet is invisible: tfplugindocs omits the Example Usage heading entirely
// rather than rendering an empty one, so the published page simply goes from the
// description to the schema table with nothing to suggest anything is absent.
var snippetExemptions = map[string]string{}

// TestExampleSnippetCoverage asserts that every registered resource and data
// source has a non-empty example snippet where tfplugindocs looks for one.
//
// Nothing else catches this. CI's generate job compares committed docs against
// their sources and fails on a diff, but a missing snippet produces stable,
// committed docs and an empty diff, so it passes. The gap is only visible by
// reading the published page.
//
// This checks presence, not correctness: a snippet referencing an attribute that
// does not exist would satisfy it while still misleading every reader. Running
// terraform validate over each snippet is the stronger check and is tracked
// separately, since it needs a built provider and a Terraform binary.
func TestExampleSnippetCoverage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	p := provider.New("test")()

	type resourcesProvider interface {
		Resources(context.Context) []func() resource.Resource
	}
	type dataSourcesProvider interface {
		DataSources(context.Context) []func() datasource.DataSource
	}

	rp, ok := p.(resourcesProvider)
	if !ok {
		t.Fatal("provider does not implement Resources()")
	}
	dp, ok := p.(dataSourcesProvider)
	if !ok {
		t.Fatal("provider does not implement DataSources()")
	}

	// Type names come from each type's own Metadata(), which is also what
	// tfplugindocs uses to find the directory. Deriving the name rather than
	// keeping a list is the point: a type added tomorrow is covered without
	// anyone remembering to update this test.
	var checked int

	for _, factory := range rp.Resources(ctx) {
		var meta resource.MetadataResponse
		factory().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "datahub"}, &meta)
		checkSnippet(t, meta.TypeName, "resources", "resource.tf")
		checked++
	}

	for _, factory := range dp.DataSources(ctx) {
		var meta datasource.MetadataResponse
		factory().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "datahub"}, &meta)
		checkSnippet(t, meta.TypeName, "data-sources", "data-source.tf")
		checked++
	}

	// Guard against a vacuous pass: if the provider interfaces ever change shape
	// and the loops silently iterate nothing, every assertion above is skipped
	// and the test still reports success.
	const wantAtLeast = 60
	if checked < wantAtLeast {
		t.Errorf("only %d types checked, expected at least %d; the provider's "+
			"Resources()/DataSources() enumeration is probably not being read correctly",
			checked, wantAtLeast)
	}
}

// checkSnippet fails the test when the example file for typeName is missing or
// empty and no exemption covers it.
func checkSnippet(t *testing.T, typeName, dir, filename string) {
	t.Helper()

	path := filepath.Join("..", "..", "examples", dir, typeName, filename)

	st, err := os.Stat(path)
	// Non-empty rather than merely present: an empty file satisfies existence and
	// still renders no Example Usage section, which is the failure being guarded.
	if err == nil && st.Size() > 0 {
		return
	}
	if reason, exempt := snippetExemptions[typeName]; exempt {
		t.Logf("%s: no example snippet, exempt (%s)", typeName, reason)
		return
	}

	state := "is empty"
	if os.IsNotExist(err) {
		state = "does not exist"
	}
	t.Errorf("%s has no example snippet: %s %s. Its registry page will render no "+
		"Example Usage section at all, with nothing to indicate anything is missing. "+
		"Add the file, or add an entry to snippetExemptions with a reason.",
		typeName, path, state)
}
