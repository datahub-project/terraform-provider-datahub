// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"testing"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// The merge contract of datahub_entity_ownership reduces entirely to the three
// functions exercised here, so they are tested directly rather than only
// through an acceptance run: a mock-backed test tells you the resource behaved,
// while these say which claim broke when it does not.

func edge(owner, typeSuffix string) datahub.OwnerEdge {
	return datahub.OwnerEdge{
		OwnerURN:         owner,
		OwnershipTypeURN: "urn:li:ownershipType:" + typeSuffix,
	}
}

// edgeNames renders edges for a failure message in a form that is short enough
// to read and unambiguous enough to act on.
func edgeNames(edges []datahub.OwnerEdge) []string {
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		out = append(out, e.OwnerURN+"/"+e.OwnershipTypeURN)
	}
	return out
}

func sameEdges(got, want []datahub.OwnerEdge) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

const (
	alice = "urn:li:corpuser:alice"
	bob   = "urn:li:corpuser:bob"
	fin   = "urn:li:corpGroup:finance"
)

func TestDiffOwnerEdges(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		prior      []datahub.OwnerEdge
		desired    []datahub.OwnerEdge
		wantAdd    []datahub.OwnerEdge
		wantRemove []datahub.OwnerEdge
	}{
		"create from nothing": {
			prior:      nil,
			desired:    []datahub.OwnerEdge{edge(alice, "steward"), edge(fin, "steward")},
			wantAdd:    []datahub.OwnerEdge{edge(alice, "steward"), edge(fin, "steward")},
			wantRemove: []datahub.OwnerEdge{},
		},
		"no change": {
			prior:      []datahub.OwnerEdge{edge(alice, "steward")},
			desired:    []datahub.OwnerEdge{edge(alice, "steward")},
			wantAdd:    []datahub.OwnerEdge{},
			wantRemove: []datahub.OwnerEdge{},
		},
		"reordering is not a change": {
			prior:      []datahub.OwnerEdge{edge(alice, "steward"), edge(fin, "producer")},
			desired:    []datahub.OwnerEdge{edge(fin, "producer"), edge(alice, "steward")},
			wantAdd:    []datahub.OwnerEdge{},
			wantRemove: []datahub.OwnerEdge{},
		},
		"add one remove one keep one in a single apply": {
			prior:      []datahub.OwnerEdge{edge(alice, "steward"), edge(fin, "producer")},
			desired:    []datahub.OwnerEdge{edge(alice, "steward"), edge(bob, "producer")},
			wantAdd:    []datahub.OwnerEdge{edge(bob, "producer")},
			wantRemove: []datahub.OwnerEdge{edge(fin, "producer")},
		},
		// Two owners in one ownership type. In the dataset that drove this
		// resource, 92 of 1,782 owner cells hold two owners in the same role, so
		// this is the primary use case: adding the second must not be read as
		// replacing the first.
		"second owner joins an existing ownership type": {
			prior:      []datahub.OwnerEdge{edge(alice, "steward")},
			desired:    []datahub.OwnerEdge{edge(alice, "steward"), edge(bob, "steward")},
			wantAdd:    []datahub.OwnerEdge{edge(bob, "steward")},
			wantRemove: []datahub.OwnerEdge{},
		},
		// One owner in two ownership types. A diff keyed on the owner URN would
		// report nothing to add here, or worse, a removal.
		"same owner gains a second ownership type": {
			prior:      []datahub.OwnerEdge{edge(alice, "steward")},
			desired:    []datahub.OwnerEdge{edge(alice, "steward"), edge(alice, "producer")},
			wantAdd:    []datahub.OwnerEdge{edge(alice, "producer")},
			wantRemove: []datahub.OwnerEdge{},
		},
		// The optional-ownershipTypeUrn trap, at the diff level: only the named
		// pair may be removed, never the owner's other role.
		"dropping one of an owner's two ownership types keeps the other": {
			prior:      []datahub.OwnerEdge{edge(alice, "steward"), edge(alice, "producer")},
			desired:    []datahub.OwnerEdge{edge(alice, "producer")},
			wantAdd:    []datahub.OwnerEdge{},
			wantRemove: []datahub.OwnerEdge{edge(alice, "steward")},
		},
		"destroying every declared pair": {
			prior:      []datahub.OwnerEdge{edge(alice, "steward"), edge(fin, "steward")},
			desired:    nil,
			wantAdd:    []datahub.OwnerEdge{},
			wantRemove: []datahub.OwnerEdge{edge(alice, "steward"), edge(fin, "steward")},
		},
		"duplicates in either input collapse": {
			prior:      []datahub.OwnerEdge{edge(alice, "steward"), edge(alice, "steward")},
			desired:    []datahub.OwnerEdge{edge(alice, "steward"), edge(alice, "steward"), edge(bob, "steward")},
			wantAdd:    []datahub.OwnerEdge{edge(bob, "steward")},
			wantRemove: []datahub.OwnerEdge{},
		},
		"same owner and type differing only by entity is still one pair": {
			prior:      []datahub.OwnerEdge{edge(alice, "steward")},
			desired:    []datahub.OwnerEdge{edge(alice, "Steward")},
			wantAdd:    []datahub.OwnerEdge{edge(alice, "Steward")},
			wantRemove: []datahub.OwnerEdge{edge(alice, "steward")},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			add, remove := diffOwnerEdges(tc.prior, tc.desired)
			if !sameEdges(add, tc.wantAdd) {
				t.Errorf("add = %v, want %v", edgeNames(add), edgeNames(tc.wantAdd))
			}
			if !sameEdges(remove, tc.wantRemove) {
				t.Errorf("remove = %v, want %v", edgeNames(remove), edgeNames(tc.wantRemove))
			}
		})
	}
}

// TestDiffOwnerEdgesNeverTouchesUndeclaredPairs is the merge contract stated as
// a property rather than as a case: whatever the inputs, neither output may
// contain a pair absent from both of them. prior comes from state and desired
// from the plan, so a pair in neither is one somebody else assigned.
func TestDiffOwnerEdgesNeverTouchesUndeclaredPairs(t *testing.T) {
	t.Parallel()

	prior := []datahub.OwnerEdge{edge(alice, "steward"), edge(fin, "producer")}
	desired := []datahub.OwnerEdge{edge(bob, "steward")}
	outOfBand := edge("urn:li:corpuser:carol", "technical")

	add, remove := diffOwnerEdges(prior, desired)
	for _, e := range append(append([]datahub.OwnerEdge{}, add...), remove...) {
		if e == outOfBand {
			t.Fatalf("diff produced a pair that was in neither prior nor desired: %v", e)
		}
	}
}

func TestRetainPresentOwnerEdges(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		declared []datahub.OwnerEdge
		actual   []datahub.OwnerEdge
		want     []datahub.OwnerEdge
	}{
		"all declared pairs still present": {
			declared: []datahub.OwnerEdge{edge(alice, "steward"), edge(fin, "steward")},
			actual:   []datahub.OwnerEdge{edge(fin, "steward"), edge(alice, "steward")},
			want:     []datahub.OwnerEdge{edge(alice, "steward"), edge(fin, "steward")},
		},
		// The reason Read narrows rather than adopts: an owner somebody assigned
		// in the UI must not enter state, or the next destroy would delete it.
		"owners present but undeclared are discarded": {
			declared: []datahub.OwnerEdge{edge(alice, "steward")},
			actual:   []datahub.OwnerEdge{edge(alice, "steward"), edge(bob, "technical")},
			want:     []datahub.OwnerEdge{edge(alice, "steward")},
		},
		"a declared pair deleted out of band drops out of state": {
			declared: []datahub.OwnerEdge{edge(alice, "steward"), edge(fin, "steward")},
			actual:   []datahub.OwnerEdge{edge(alice, "steward")},
			want:     []datahub.OwnerEdge{edge(alice, "steward")},
		},
		"an entity whose owners were all cleared": {
			declared: []datahub.OwnerEdge{edge(alice, "steward")},
			actual:   nil,
			want:     []datahub.OwnerEdge{},
		},
		"order follows declared, not the server's array order": {
			declared: []datahub.OwnerEdge{edge(fin, "producer"), edge(alice, "steward")},
			actual:   []datahub.OwnerEdge{edge(alice, "steward"), edge(fin, "producer")},
			want:     []datahub.OwnerEdge{edge(fin, "producer"), edge(alice, "steward")},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := retainPresentOwnerEdges(tc.declared, tc.actual)
			if !sameEdges(got, tc.want) {
				t.Errorf("retainPresentOwnerEdges = %v, want %v", edgeNames(got), edgeNames(tc.want))
			}
		})
	}
}

func TestMissingOwnerEdges(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		wanted []datahub.OwnerEdge
		actual []datahub.OwnerEdge
		want   []datahub.OwnerEdge
	}{
		"everything landed": {
			wanted: []datahub.OwnerEdge{edge(alice, "steward"), edge(fin, "steward")},
			actual: []datahub.OwnerEdge{edge(fin, "steward"), edge(alice, "steward"), edge(bob, "technical")},
			want:   []datahub.OwnerEdge{},
		},
		"one pair silently dropped": {
			wanted: []datahub.OwnerEdge{edge(alice, "steward"), edge(fin, "steward")},
			actual: []datahub.OwnerEdge{edge(alice, "steward")},
			want:   []datahub.OwnerEdge{edge(fin, "steward")},
		},
		"the write was a complete no-op": {
			wanted: []datahub.OwnerEdge{edge(alice, "steward")},
			actual: nil,
			want:   []datahub.OwnerEdge{edge(alice, "steward")},
		},
		// The owner landed, but under a different ownership type. Comparing on
		// the owner URN alone would call this a success.
		"right owner, wrong ownership type": {
			wanted: []datahub.OwnerEdge{edge(alice, "steward")},
			actual: []datahub.OwnerEdge{edge(alice, "producer")},
			want:   []datahub.OwnerEdge{edge(alice, "steward")},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := missingOwnerEdges(tc.wanted, tc.actual)
			if !sameEdges(got, tc.want) {
				t.Errorf("missingOwnerEdges = %v, want %v", edgeNames(got), edgeNames(tc.want))
			}
		})
	}
}

func TestDedupeOwnerEdges(t *testing.T) {
	t.Parallel()

	got := dedupeOwnerEdges([]datahub.OwnerEdge{
		edge(alice, "steward"),
		edge(fin, "steward"),
		edge(alice, "steward"),
		edge(alice, "producer"),
	})
	want := []datahub.OwnerEdge{edge(alice, "steward"), edge(fin, "steward"), edge(alice, "producer")}
	if !sameEdges(got, want) {
		t.Errorf("dedupeOwnerEdges = %v, want %v (first-seen order preserved)", edgeNames(got), edgeNames(want))
	}
}
