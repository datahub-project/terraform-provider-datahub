// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// The three functions here are the whole of datahub_entity_ownership's merge
// contract, kept pure and free of the plugin framework so the semantics can be
// unit-tested without a mock server or a Terraform run. Every interesting claim
// the resource makes -- that it adds only what is newly declared, removes only
// what it previously declared, and never touches an owner added out of band --
// reduces to one of these.

// dedupeOwnerEdges returns edges with exact duplicates removed, preserving
// first-seen order.
//
// A Terraform set cannot hold two identical elements, so an exactly duplicated
// owner block collapses before the provider ever sees it. This exists for the
// other direction: the server's owners array is deduplicated by (owner, type)
// on write, but nothing in the read path guarantees that, and a duplicate there
// would otherwise produce a duplicated remove call on destroy.
func dedupeOwnerEdges(edges []datahub.OwnerEdge) []datahub.OwnerEdge {
	seen := make(map[datahub.OwnerEdge]struct{}, len(edges))
	out := make([]datahub.OwnerEdge, 0, len(edges))
	for _, e := range edges {
		if _, dup := seen[e]; dup {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	return out
}

// diffOwnerEdges computes what an update must write, given the pairs this
// resource declared last time (prior, from state) and the pairs it declares now
// (desired, from the plan).
//
// add is every desired pair not already in prior; remove is every prior pair no
// longer desired. Pairs in both are left alone. Crucially, neither list can
// contain a pair this resource never declared, which is what makes an owner
// added in the DataHub UI survive: it appears in neither input.
//
// Both outputs preserve their input's order so the calls a given plan makes are
// deterministic and a failing test names the same pair every run.
//
// The identity being compared is the whole (owner, ownership type) pair.
// Comparing on either half alone would be wrong in a way that loses data:
// several owners routinely share one ownership type, and one owner routinely
// holds several, so a diff keyed on the owner URN would delete a role the
// configuration still declares.
func diffOwnerEdges(prior, desired []datahub.OwnerEdge) (add, remove []datahub.OwnerEdge) {
	prior = dedupeOwnerEdges(prior)
	desired = dedupeOwnerEdges(desired)

	priorSet := make(map[datahub.OwnerEdge]struct{}, len(prior))
	for _, e := range prior {
		priorSet[e] = struct{}{}
	}
	desiredSet := make(map[datahub.OwnerEdge]struct{}, len(desired))
	for _, e := range desired {
		desiredSet[e] = struct{}{}
	}

	add = make([]datahub.OwnerEdge, 0, len(desired))
	for _, e := range desired {
		if _, held := priorSet[e]; !held {
			add = append(add, e)
		}
	}
	remove = make([]datahub.OwnerEdge, 0, len(prior))
	for _, e := range prior {
		if _, wanted := desiredSet[e]; !wanted {
			remove = append(remove, e)
		}
	}
	return add, remove
}

// missingOwnerEdges returns the wanted pairs that actual does not contain. It
// backs Create's read-back guard: batchAddOwners answers with a nullable
// Boolean and the aspect write happens behind it, so only asking the server
// afterwards distinguishes "written" from "accepted and dropped".
func missingOwnerEdges(wanted, actual []datahub.OwnerEdge) []datahub.OwnerEdge {
	actualSet := make(map[datahub.OwnerEdge]struct{}, len(actual))
	for _, e := range actual {
		actualSet[e] = struct{}{}
	}

	out := make([]datahub.OwnerEdge, 0)
	for _, e := range dedupeOwnerEdges(wanted) {
		if _, present := actualSet[e]; !present {
			out = append(out, e)
		}
	}
	return out
}

// retainPresentOwnerEdges narrows the pairs this resource declared (declared,
// from state) to those the server still reports (actual, from a fresh read).
// This is the Read path: state records what the resource owns, so a pair
// deleted out of band must drop out of state and reappear as a plan to add it
// back.
//
// Order follows declared, not actual, so refresh does not churn state on a
// server that returns the owners array in a different order than it was written
// -- irrelevant to a set in principle, and free to get right here.
//
// Owners present in actual but not declared are deliberately discarded: they
// belong to whoever added them.
func retainPresentOwnerEdges(declared, actual []datahub.OwnerEdge) []datahub.OwnerEdge {
	actualSet := make(map[datahub.OwnerEdge]struct{}, len(actual))
	for _, e := range actual {
		actualSet[e] = struct{}{}
	}

	out := make([]datahub.OwnerEdge, 0, len(declared))
	for _, e := range dedupeOwnerEdges(declared) {
		if _, present := actualSet[e]; present {
			out = append(out, e)
		}
	}
	return out
}
