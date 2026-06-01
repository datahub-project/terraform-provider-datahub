// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"strings"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget"
)

func init() {
	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_local_user_login",
		DataSourceTypeName: "",
		Enumerate:          nil,
		IDFromURN: func(urn string) string {
			return strings.TrimPrefix(urn, corpUserURNPrefix)
		},
		OSSCompatible: true,
	})
}
