// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

func init() {
	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_connection",
		DataSourceTypeName: "datahub_connections",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			all, err := c.ListConnectionURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing connection URNs: %w", err)
			}
			// Skip system and OAuth connections that the provider cannot manage.
			// DataHub Cloud creates internal connections with IDs like
			// "urn_li_corpuser_alice@example.com__urn_li_service_<uuid>" (OAuth)
			// and "__system_teams-0" (system). These have unknown platform types
			// that the datahub_connection resource cannot model, so attempting to
			// import them fails with a provider error and aborts generate-config-out.
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
}
