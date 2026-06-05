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
		ResourceTypeName:   "datahub_glossary_node",
		DataSourceTypeName: "datahub_glossary_nodes",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			urns, err := c.ListGlossaryNodeURNs(ctx)
			if err != nil {
				return nil, fmt.Errorf("listing glossary node URNs: %w", err)
			}
			return urns, nil
		},
		IDFromURN:     func(urn string) string { return strings.TrimPrefix(urn, glossaryNodeURNPrefix) },
		OSSCompatible: true,
	})
}
