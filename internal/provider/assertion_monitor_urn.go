// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
)

// monitorURNSchema returns the shared schema for the computed monitor_urn
// attribute carried by every Cloud monitor-backed assertion resource
// (freshness, volume, sql, field, schema).
//
// The attribute exists so destroy does not depend on an eventually-consistent
// lookup: DataHub's deleteAssertion mutation leaves the Monitor entity behind,
// so the provider must delete it explicitly, and resolving the monitor at
// delete time uses a graph-backed query that can transiently fail or miss.
// Persisting the URN at create time -- and refreshing it on read, which is also
// how imported and pre-upgrade states acquire it -- gives Delete a reference
// that does not need resolving. An orphaned monitor counts toward a DataHub
// Cloud tenant's monitor limit and blocks recreating an assertion of the same
// type on the same dataset; orphan accumulation has exhausted tenant limits in
// production (OBS-2077).
func monitorURNSchema() schema.StringAttribute {
	return schema.StringAttribute{
		Computed: true,
		MarkdownDescription: "URN of the DataHub Monitor entity backing this assertion " +
			"(e.g. `urn:li:monitor:<id>`). Captured when the assertion is created and " +
			"refreshed on read. The provider deletes this monitor explicitly on destroy: " +
			"DataHub's `deleteAssertion` leaves the monitor in place, and an orphaned " +
			"monitor counts toward the DataHub Cloud monitor limit and blocks recreating " +
			"an assertion of the same type on the same dataset.",
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.UseStateForUnknown(),
		},
	}
}
