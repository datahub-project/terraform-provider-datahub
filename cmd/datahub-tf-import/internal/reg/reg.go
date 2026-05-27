// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// Package reg registers all DataHub import targets with the importtarget
// registry. Import this package for its side effects:
//
//	import _ "github.com/datahub-project/terraform-provider-datahub/cmd/datahub-tf-import/internal/reg"
//
// This package does not import the Terraform plugin framework, keeping the
// datahub-tf-import binary free of provider-runtime dependencies.
package reg

import (
	"context"
	"strings"

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
			return urn
		},
		OSSCompatible: true,
	})

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

	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_ingestion_source",
		DataSourceTypeName: "datahub_ingestion_sources",
		Enumerate: func(ctx context.Context, c *datahub.Client) ([]string, error) {
			return c.ListIngestionSourceURNs(ctx)
		},
		IDFromURN: func(urn string) string {
			return strings.TrimPrefix(urn, "urn:li:dataHubIngestionSource:")
		},
		OSSCompatible: true,
	})

	importtarget.Register(importtarget.Target{
		ResourceTypeName: "datahub_remote_executor_pool",
		// No DataSourceTypeName: no list API available on Cloud.
		// No Enumerate: Cloud-only; users supply pool IDs manually.
		IDFromURN: func(urn string) string {
			return strings.TrimPrefix(urn, "urn:li:dataHubRemoteExecutorPool:")
		},
		OSSCompatible: false,
	})
}
