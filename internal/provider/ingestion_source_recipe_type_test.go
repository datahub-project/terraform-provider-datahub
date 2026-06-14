// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider"
)

// TestIngestionSourceRecipeUsesNormalizedType asserts that the recipe attribute
// is declared with jsontypes.NormalizedType. This is what lets the provider
// reconcile a recipe that DataHub returns in a different JSON formatting/key
// order than the Terraform config (via JSON semantic equality in Create/Read/
// Update), so an imported ingestion source converges to a clean plan after one
// apply instead of drifting on recipe whitespace forever. A regression to a
// plain string type would silently reintroduce that perpetual drift.
//
// Note: semantic equality is applied by terraform-plugin-framework during
// Create/Read/Update reconciliation, not during plan diffing -- so the very
// first plan immediately after import still shows a one-time whitespace diff
// that the first apply clears. See docs/guides/import-existing.md.
func TestIngestionSourceRecipeUsesNormalizedType(t *testing.T) {
	t.Parallel()

	var resp resource.SchemaResponse
	provider.NewIngestionSourceResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)

	attr, ok := resp.Schema.Attributes["recipe"].(schema.StringAttribute)
	if !ok {
		t.Fatalf("recipe attribute is %T, want schema.StringAttribute", resp.Schema.Attributes["recipe"])
	}
	if _, ok := attr.CustomType.(jsontypes.NormalizedType); !ok {
		t.Errorf("recipe CustomType = %T, want jsontypes.NormalizedType", attr.CustomType)
	}
}
