// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

func init() {
	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_secret",
		DataSourceTypeName: "datahub_secrets",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			return c.ListSecretURNs(ctx)
		},
		IDFromURN: func(urn string) string {
			// ImportState accepts both the full URN and the bare name; we pass
			// the full URN so the provider can validate the prefix.
			return urn
		},
		OSSCompatible: true,
	})
}
