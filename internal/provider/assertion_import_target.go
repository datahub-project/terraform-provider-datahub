// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

func init() {
	// Custom assertions are OSS-compatible and support enumeration for bulk import.
	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_custom_assertion",
		DataSourceTypeName: "datahub_assertions",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListAssertionURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing assertion URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return urn },
		OSSCompatible: true,
	})

	// Cloud-only assertion types: no enumeration function since they share
	// the same entity type as custom assertions and the type cannot be
	// filtered at the list layer. Import by explicit URN.
	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_freshness_assertion",
		DataSourceTypeName: "datahub_assertions",
		Enumerate:          nil,
		IDFromURN:          func(urn string) string { return urn },
		OSSCompatible:      false,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_volume_assertion",
		DataSourceTypeName: "datahub_assertions",
		Enumerate:          nil,
		IDFromURN:          func(urn string) string { return urn },
		OSSCompatible:      false,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_sql_assertion",
		DataSourceTypeName: "datahub_assertions",
		Enumerate:          nil,
		IDFromURN:          func(urn string) string { return urn },
		OSSCompatible:      false,
	})
}
