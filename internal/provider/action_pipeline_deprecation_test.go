// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// TestActionPipelineNoEffectAttributesAreDeprecated pins the deprecation of
// version and debug_mode. DataHub has never stored either value, so the only
// thing a configuration setting them gets is a perpetual diff; the warning is
// what tells the practitioner to remove them before the attributes go.
func TestActionPipelineNoEffectAttributesAreDeprecated(t *testing.T) {
	resp := &resource.SchemaResponse{}
	NewActionPipelineResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics)
	}

	for _, name := range []string{"version", "debug_mode"} {
		attr, ok := resp.Schema.Attributes[name]
		if !ok {
			t.Fatalf("attribute %q missing from the schema", name)
		}
		if attr.GetDeprecationMessage() != actionPipelineNoEffectDeprecation {
			t.Errorf("attribute %q: deprecation message = %q, want the no-effect deprecation", name, attr.GetDeprecationMessage())
		}
	}
}
