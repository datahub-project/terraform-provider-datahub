// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget"
)

func init() {
	// Membership edges are not independently enumerable as a single URN list,
	// so this target registers without an Enumerate function or companion
	// enumeration data source. The coverage test is satisfied by the presence
	// of the entry. The import ID is the composite "<group_urn>|<user_urn>",
	// so IDFromURN (single-URN extraction) does not apply.
	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_corp_group_member",
		DataSourceTypeName: "",
		Enumerate:          nil,
		IDFromURN:          nil,
		OSSCompatible:      true,
	})
}
