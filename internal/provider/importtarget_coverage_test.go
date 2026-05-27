// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"

	provider "github.com/datahub-project/terraform-provider-datahub/internal/provider"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget"
)

// exemptions maps resource type names that are intentionally absent from the
// importtarget registry to a human-readable reason. Add an entry here when a
// new resource genuinely cannot support auto-enumeration (e.g. Cloud-only with
// no list API) rather than silently omitting it.
var exemptions = map[string]string{
	// No exemptions at this time. Remote executor pools have a registry entry
	// with Enumerate: nil (Cloud-only, no list API in OSS).
}

// TestImportTargetCoverage asserts that every resource registered with the
// provider either has an entry in importtarget.All() or an explicit exemption
// in the table above. This test prevents new resources from being added without
// a deliberate import-target decision.
func TestImportTargetCoverage(t *testing.T) {
	t.Parallel()

	// Build a set of registered resource type names.
	registered := make(map[string]bool)
	for _, tgt := range importtarget.All() {
		registered[tgt.ResourceTypeName] = true
	}

	// Enumerate every resource the provider exposes.
	p := provider.New("test")()
	type resourcesProvider interface {
		Resources(context.Context) []func() resource.Resource
	}
	rp, ok := p.(resourcesProvider)
	if !ok {
		t.Fatal("provider does not implement Resources()")
	}

	ctx := context.Background()
	for _, factory := range rp.Resources(ctx) {
		r := factory()

		var metaResp resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "datahub"}, &metaResp)
		typeName := metaResp.TypeName

		if registered[typeName] {
			continue
		}
		if reason, exempt := exemptions[typeName]; exempt {
			t.Logf("resource %s: exempt from importtarget registry (%s)", typeName, reason)
			continue
		}
		t.Errorf(
			"resource %q is not registered in importtarget.All() and has no exemption entry; "+
				"add a registration in internal/provider/<resource>_import_target.go or add it to "+
				"the exemptions table in importtarget_coverage_test.go",
			typeName,
		)
	}
}
