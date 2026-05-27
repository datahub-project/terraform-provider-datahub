// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"strings"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

func init() {
	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_connection",
		DataSourceTypeName: "datahub_connections",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			return c.ListConnectionURNs(ctx)
		},
		IDFromURN: func(urn string) string {
			return strings.TrimPrefix(urn, "urn:li:dataHubConnection:")
		},
		OSSCompatible: true,
	})
}
