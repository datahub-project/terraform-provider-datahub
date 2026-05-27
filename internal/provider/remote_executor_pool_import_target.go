// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"strings"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget"
)

func init() {
	importtarget.Register(importtarget.Target{
		ResourceTypeName: "datahub_remote_executor_pool",
		// DataSourceTypeName is empty: no enumeration data source yet for
		// remote executor pools. DataHub Cloud does not expose a list API for
		// this Cloud-only entity type that is accessible via OSS GraphQL.
		// Users specify pool IDs manually when importing.
		DataSourceTypeName: "",
		// Enumerate is nil: auto-enumeration is not supported. The CLI skips
		// this type and instructs the user to supply pool IDs directly.
		Enumerate: nil,
		IDFromURN: func(urn string) string {
			return strings.TrimPrefix(urn, "urn:li:dataHubRemoteExecutorPool:")
		},
		OSSCompatible: false,
	})
}
