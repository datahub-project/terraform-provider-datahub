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
	"fmt"
	"strings"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

func init() {
	// Register types with no Required+WriteOnly attributes first so that
	// terraform plan -generate-config-out generates their blocks before
	// encountering the datahub_secret exit-1 error (Required value=null).
	// Terraform stops after the first fatal plan error, so types registered
	// later would be silently omitted from generated.tf if secrets came first.

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

	importtarget.Register(importtarget.Target{
		ResourceTypeName: "datahub_remote_executor_pool",
		// No DataSourceTypeName: no list API available on Cloud.
		// No Enumerate: Cloud-only; users supply pool IDs manually.
		IDFromURN: func(urn string) string {
			return strings.TrimPrefix(urn, "urn:li:dataHubRemoteExecutorPool:")
		},
		OSSCompatible: false,
	})

	// datahub_secret last: its Required+WriteOnly value attribute causes
	// terraform plan -generate-config-out to exit with code 1. Placing
	// secrets at the end ensures all other resource types are fully
	// generated before Terraform hits that error.
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
}
