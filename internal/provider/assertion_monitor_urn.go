// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
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
			"refreshed on read. DataHub links a new monitor to its assertion asynchronously; " +
			"if it has not done so within a few minutes of the create, the apply succeeds " +
			"with a warning, this attribute stays empty, and the next refresh fills it in " +
			"without changing the assertion. The provider deletes this monitor explicitly on destroy: " +
			"DataHub's `deleteAssertion` leaves the monitor in place, and an orphaned " +
			"monitor counts toward the DataHub Cloud monitor limit and blocks recreating " +
			"an assertion of the same type on the same dataset.",
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.UseStateForUnknown(),
		},
	}
}

// acceptMonitorPending turns the *datahub.MonitorPendingError a typed
// assertion upsert returns on create into a warning, and returns nil so the
// caller goes on to record the assertion in state with monitor_urn null. Any
// other error is returned unchanged.
//
// Failing the create instead is what used to leak: the assertion and its
// monitor both exist by the time the link is awaited, so an error here left
// them on the server but not in state, and the next apply tried to create a
// second pair. Recording the assertion is safe because monitor_urn is Computed
// and planned unknown on create, so a null after apply is a valid result, and
// Read fills it from the same lookup once DataHub has indexed the link -- a
// refresh change to a computed attribute, not a planned diff.
func acceptMonitorPending(err error, resourceType string, diags *diag.Diagnostics) error {
	var pending *datahub.MonitorPendingError
	if !errors.As(err, &pending) {
		return err
	}
	diags.AddWarning(
		"Assertion monitor not yet linked",
		fmt.Sprintf("The %s %s was created, and DataHub created its monitor in the same request, "+
			"but DataHub had not finished linking the monitor to the assertion when the provider "+
			"stopped waiting (%v).\n\n"+
			"The assertion has been recorded in Terraform state with monitor_urn empty. Nothing "+
			"needs to be done: the next refresh (any terraform plan or apply) fills monitor_urn "+
			"in once DataHub catches up, without changing the assertion.\n\n"+
			"If monitor_urn is still empty after several minutes, DataHub is not processing "+
			"updates; check the assertion in the DataHub UI. Avoid destroying the resource "+
			"while monitor_urn is empty, because the provider may not be able to find the "+
			"monitor to delete it.",
			resourceType, pending.AssertionURN, pending.Cause),
	)
	return nil
}

// recordCreatedAssertionOnError writes a just-created assertion into state
// before reporting a failure in a later step of Create (such as applying
// tags). Terraform keeps a resource whose create returned both state and an
// error, marked tainted, so the next apply replaces it through Delete -- which
// removes the monitor too -- instead of creating a second assertion beside an
// untracked one.
func recordCreatedAssertionOnError(ctx context.Context, state *tfsdk.State, model any, diags *diag.Diagnostics, err error) {
	diags.Append(state.Set(ctx, model)...)
	diags.AddError("DataHub API Error", err.Error())
}

// deleteAssertionAndMonitor deletes a typed assertion and its monitor, and
// warns when the monitor could not be identified. It reports whether the
// delete succeeded; on failure the error is already in diags.
//
// monitorURN null means the URN was never recorded: state written before
// monitor_urn existed, or a create that finished before DataHub linked the
// monitor and no refresh since has picked it up. The client then falls back
// to the same lookup, and if that also comes back empty the assertion is
// deleted alone. That is not proof the assertion had no monitor, only that the
// link is still not visible, so the destroy goes ahead -- blocking it would
// strand the resource for a lag that resolves itself -- with a warning that a
// monitor may remain.
func deleteAssertionAndMonitor(ctx context.Context, client *datahub.Client, resourceType, assertionURN string, monitorURN types.String, diags *diag.Diagnostics) bool {
	deleted, err := client.DeleteCloudAssertionWithMonitor(ctx, assertionURN, monitorURN.ValueString())
	if err != nil {
		diags.AddError("DataHub API Error", err.Error())
		return false
	}
	if deleted == "" {
		diags.AddWarning(
			"Assertion monitor may remain",
			fmt.Sprintf("The %s %s was deleted, but its monitor could not be identified: monitor_urn "+
				"was not recorded in state and DataHub did not report a monitor linked to the "+
				"assertion. If the assertion was created only moments ago, DataHub may not yet have "+
				"linked its monitor, and that monitor has been left behind. A leftover monitor counts "+
				"toward the DataHub Cloud monitor limit and can block creating another assertion of "+
				"the same type on the same dataset. Its URN contains the dataset URN, and it can be "+
				"deleted with DELETE /openapi/v3/entity/monitor/{urn} once DataHub lists it.",
				resourceType, assertionURN),
		)
	}
	return true
}
