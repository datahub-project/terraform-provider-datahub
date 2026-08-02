// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// Server-bug mitigation, tracked upstream as OSS-1216.
//
// The dataHubPolicyInfo aspect carries two fields that the updatePolicy
// mutation cannot express: actors.roles (DataHub role URNs the policy grants
// to) and editable. PolicyUpdateInputInfoMapper builds a fresh
// DataHubPolicyInfo from PolicyUpdateInput and copies across only
// description, type, displayName, privileges, actors, state and resources.
// ActorFilterInput has no roles field and PolicyUpdateInput has no editable
// field, so every updatePolicy call -- from this provider, from the DataHub
// UI, from any other client -- deletes actors.roles and resets editable to
// its PDL default of true.
//
// Dropping roles is destructive rather than merely lossy. DataHub's own
// bootstrap policies bind their actors through roles alone: nine of the
// sixteen in datahub-upgrade's boot/policies.json name no users, no groups
// and set neither allUsers nor allGroups. Strip the roles and the policy
// grants its privileges to nobody. Repair needs an OpenAPI v3 aspect write,
// because saving the policy in the UI goes through the same mutation and
// would strip it again -- and if the gutted policy was the one carrying
// MANAGE_POLICIES, there may be no principal left who can perform the repair.
// The exposure is not limited to DataHub's own policies: the aspect supports
// roles and the Python SDK writes them, so any policy-as-code estate can hold
// role-bearing policies of its own.
//
// The provider cannot fix this. What it can do is refuse to be the caller
// that triggers it, which is what the diagnostics below are for. Remove them
// once ActorFilterInput carries roles and the mapper preserves them.

// policyLabel renders a policy for a diagnostic: display name plus URN, or
// just the URN when the policy has no display name.
func policyLabel(p *datahub.Policy) string {
	if strings.TrimSpace(p.Name) == "" {
		return p.URN
	}
	return fmt.Sprintf("%q (%s)", p.Name, p.URN)
}

// policyRolesAreSoleBinding reports whether the policy's roles are the only
// thing binding it to any actor. When they are, stripping them does not narrow
// the policy -- it empties it.
func policyRolesAreSoleBinding(p *datahub.Policy) bool {
	a := p.Actors
	return len(a.Users) == 0 && len(a.Groups) == 0 &&
		!a.AllUsers && !a.AllGroups && !a.ResourceOwners
}

// policyRoleLossDescription is the shared opening paragraph of both the write
// refusal and the import warning: what the policy grants through roles, and
// what losing them would cost.
func policyRoleLossDescription(p *datahub.Policy) string {
	s := fmt.Sprintf("Policy %s grants access through DataHub roles: %s.",
		policyLabel(p), strings.Join(p.ActorRoles, ", "))
	if policyRolesAreSoleBinding(p) {
		s += " Those roles are the policy's only actor binding -- it names no users and no " +
			"groups, and applies to neither all users, all groups, nor resource owners -- so " +
			"removing them would not narrow the policy, it would leave it granting its " +
			"privileges to nobody."
	} else {
		s += " The policy's other actor bindings would survive a write; the role grants would not."
	}
	return s
}

// policyRoleStripError is the refusal returned instead of sending a write that
// would delete the policy's role bindings.
func policyRoleStripError(p *datahub.Policy) diag.Diagnostic {
	return diag.NewErrorDiagnostic(
		"Policy grants access through roles that this write would delete",
		policyRoleLossDescription(p)+"\n\n"+

			"The write has been refused rather than sent. DataHub's updatePolicy mutation is the "+
			"only supported way to write a policy, its ActorFilterInput has no roles field, and "+
			"the server rebuilds the whole dataHubPolicyInfo aspect from that input -- so any "+
			"update drops actors.roles, whatever the caller sends. Saving the policy in the "+
			"DataHub UI goes through the same mutation, so the damage could not be undone there "+
			"either. This is an upstream DataHub defect, tracked as OSS-1216; no provider "+
			"version avoids it, because the loss happens on the server.\n\n"+

			"Two ways forward:\n\n"+

			"  1. Stop managing this policy with Terraform. Delete the resource block and run "+
			"`terraform state rm` for its address, then express what you need in a policy of "+
			"your own alongside it. DataHub's default policies are returned by the "+
			"datahub_policies data source and enumerated by datahub-tf-extract together with "+
			"yours, so one of them reaching a configuration is usually accidental.\n\n"+

			"  2. If replacing the role binding with an explicit set of users and groups really "+
			"is the intent, remove actors.roles from the aspect yourself first -- POST the "+
			"dataHubPolicyInfo aspect without it to /openapi/v3/entity/datahubpolicy -- and then "+
			"re-run. Everyone holding one of those roles loses this policy's privileges at that "+
			"moment, so make sure the users and groups you are substituting are already in place.",
	)
}

// policyRoleStripImportWarning is the import-time counterpart: importing reads
// only, so it is allowed, but the user is told now rather than at the first
// apply that the resource they just adopted cannot be applied.
func policyRoleStripImportWarning(p *datahub.Policy) diag.Diagnostic {
	return diag.NewWarningDiagnostic(
		"Imported policy grants access through roles Terraform cannot preserve",
		policyRoleLossDescription(p)+"\n\n"+

			"The import itself is safe: it only reads. The first apply that writes this resource "+
			"is not, so the provider will refuse it. DataHub's updatePolicy mutation has no roles "+
			"field and the server rebuilds the whole dataHubPolicyInfo aspect from the mutation "+
			"input, which deletes actors.roles on every write. This is an upstream DataHub "+
			"defect, tracked as OSS-1216.\n\n"+

			"If adopting this policy was not deliberate -- DataHub's own default policies come "+
			"back from the datahub_policies data source and from datahub-tf-extract alongside "+
			"yours -- run `terraform state rm` for this address now and leave it out of the "+
			"configuration.",
	)
}

// policyNonEditableWarning flags a policy DataHub marks as one of its own. A
// warning rather than an error: unlike roles, nothing is destroyed. The flag
// is enforced only in the UI, and the policy keeps granting exactly what it
// granted before.
func policyNonEditableWarning(p *datahub.Policy) diag.Diagnostic {
	return diag.NewWarningDiagnostic(
		"Policy is marked non-editable by DataHub",
		fmt.Sprintf("Policy %s has editable = false in its dataHubPolicyInfo aspect. DataHub sets "+
			"that on the default policies it ships and greys them out in its UI.\n\n", policyLabel(p))+

			"Terraform can still write it -- the flag is checked in the UI only, never by the "+
			"API -- but two things follow. PolicyUpdateInput has no editable field, so any apply "+
			"resets the flag to true and the policy becomes editable in the UI from then on. And "+
			"DataHub re-ingests every non-editable default policy from its bootstrap file on "+
			"every deployment: if this is one of those, the server overwrites what Terraform "+
			"wrote each time it starts, and the following plan shows the same diff again.\n\n"+

			"Leaving DataHub's own policies to DataHub and writing a separate policy for your "+
			"own intent avoids both.",
	)
}

// policyWriteHazards inspects the live server-side policy immediately before a
// create or an update and reports what the write would cost. An error stops
// the write; a warning lets it proceed.
//
// live is nil when no policy exists at the URN yet, in which case there is
// nothing to lose and nothing to report.
func policyWriteHazards(live *datahub.Policy) diag.Diagnostics {
	var diags diag.Diagnostics
	if live == nil {
		return diags
	}
	if len(live.ActorRoles) > 0 {
		diags.Append(policyRoleStripError(live))
	}
	if !live.Editable {
		diags.Append(policyNonEditableWarning(live))
	}
	return diags
}

// policyImportHazards is the same inspection at import time, where both
// findings are warnings: import reads, it does not write.
func policyImportHazards(imported *datahub.Policy) diag.Diagnostics {
	var diags diag.Diagnostics
	if imported == nil {
		return diags
	}
	if len(imported.ActorRoles) > 0 {
		diags.Append(policyRoleStripImportWarning(imported))
	}
	if !imported.Editable {
		diags.Append(policyNonEditableWarning(imported))
	}
	return diags
}
