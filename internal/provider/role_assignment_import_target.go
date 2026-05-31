// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget"
)

func init() {
	// Role assignments are keyed by actor and have no global enumeration API
	// (it would require iterating every user and group), so this target
	// registers without an Enumerate function or companion data source. The
	// import ID is the actor URN, which is also the resource id, so IDFromURN
	// (which strips a type prefix) does not apply.
	importtarget.Register(importtarget.Target{
		ResourceTypeName:   "datahub_role_assignment",
		DataSourceTypeName: "",
		Enumerate:          nil,
		IDFromURN:          nil,
		OSSCompatible:      true,
	})
}
