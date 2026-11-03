// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"context"
	"sort"
	"testing"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// seedOwnershipTypesForImport stands up a mock DataHub holding the built-in
// ownership types alongside one customer-authored type, and returns a client
// pointed at it.
//
// The built-ins are the five observed on a DataHub Cloud v2.3.1 instance.
// __system__agent_owner is in the list on purpose: it is named neither in the
// provider documentation nor in the resource's own "Reserved type_id prefix"
// diagnostic, both of which list only technical_owner, business_owner,
// data_steward and none. A filter keyed off those four would let it through.
func seedOwnershipTypesForImport(t *testing.T) (*datahub.Client, context.Context) {
	t.Helper()

	srv := datahubtesting.NewServer(t)
	client, err := datahub.NewClient(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}
	ctx := context.Background()

	for _, id := range []string{
		"__system__technical_owner",
		"__system__business_owner",
		"__system__data_steward",
		"__system__agent_owner",
		"__system__none",
	} {
		if err := client.WriteOwnershipTypeInfo(ctx, "urn:li:ownershipType:"+id, id, "DataHub built-in"); err != nil {
			t.Fatalf("seeding built-in ownership type %q: %v", id, err)
		}
	}

	if err := client.WriteOwnershipTypeInfo(ctx,
		"urn:li:ownershipType:data_quality_lead", "Data Quality Lead", "customer authored"); err != nil {
		t.Fatalf("seeding customer ownership type: %v", err)
	}

	return client, ctx
}

// TestOwnershipTypeImportTarget_Enumerate verifies that the registered
// datahub_ownership_type import target filters out the built-in __system__
// ownership types, which the datahub_ownership_type resource refuses to manage,
// while returning customer-authored ones.
//
// Without the filter, enumeration emits import blocks the provider is
// guaranteed to reject at plan time, and `terraform plan
// -generate-config-out` fails the entire run on any single resource error
// rather than skipping it -- so one built-in takes every other resource in the
// deployment with it.
func TestOwnershipTypeImportTarget_Enumerate(t *testing.T) {
	client, ctx := seedOwnershipTypesForImport(t)

	target, ok := importtarget.ByResourceType("datahub_ownership_type")
	if !ok {
		t.Fatal("datahub_ownership_type not registered in importtarget registry")
	}
	if target.Enumerate == nil {
		t.Fatal("datahub_ownership_type has no Enumerate function")
	}

	urns, err := target.Enumerate(ctx, client)
	if err != nil {
		t.Fatalf("Enumerate: %v", err)
	}

	if len(urns) != 1 {
		t.Fatalf("Enumerate returned %d URNs, want 1 (built-in __system__ types must be filtered); got: %v", len(urns), urns)
	}
	const wantURN = "urn:li:ownershipType:data_quality_lead"
	if urns[0] != wantURN {
		t.Errorf("Enumerate returned URN %q, want %q", urns[0], wantURN)
	}
}

// TestOwnershipTypeImportTarget_ClientStillListsSystemTypes pins the layer the
// filter must NOT live in.
//
// ListOwnershipTypeURNs also backs the datahub_ownership_types data source, and
// its callers need the built-ins: ownership is attached by referencing
// __system__technical_owner and friends from datahub_entity_ownership, and the
// singular datahub_ownership_type data source exists so a built-in can be
// referenced without being taken into state. Filtering in the client would
// break that, so this test fails if a later refactor moves the enumeration
// filter one layer too low. Contrast datahub_ingestion_source, which is
// filtered in ListIngestionSourceURNs (see TestListIngestionSourceURNs_FiltersSystem)
// precisely because nothing else wants system ingestion sources.
func TestOwnershipTypeImportTarget_ClientStillListsSystemTypes(t *testing.T) {
	client, ctx := seedOwnershipTypesForImport(t)

	urns, err := client.ListOwnershipTypeURNs(ctx)
	if err != nil {
		t.Fatalf("ListOwnershipTypeURNs: %v", err)
	}

	// listOwnershipTypes returns no defined order, so compare as a set.
	got := append([]string(nil), urns...)
	sort.Strings(got)

	want := []string{
		"urn:li:ownershipType:__system__agent_owner",
		"urn:li:ownershipType:__system__business_owner",
		"urn:li:ownershipType:__system__data_steward",
		"urn:li:ownershipType:__system__none",
		"urn:li:ownershipType:__system__technical_owner",
		"urn:li:ownershipType:data_quality_lead",
	}
	if len(got) != len(want) {
		t.Fatalf("ListOwnershipTypeURNs returned %v, want %v (the client must keep listing built-ins for the data sources)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ListOwnershipTypeURNs returned %v, want %v", got, want)
		}
	}
}

// TestOwnershipTypeImportTarget_EnumerateError verifies that a DataHub API
// error is propagated (with wrapping) from the Enumerate function, rather than
// being swallowed into an empty list that would silently import nothing.
func TestOwnershipTypeImportTarget_EnumerateError(t *testing.T) {
	client, err := datahub.NewClient("http://127.0.0.1:1", "test-token")
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}

	target, ok := importtarget.ByResourceType("datahub_ownership_type")
	if !ok {
		t.Fatal("datahub_ownership_type not registered in importtarget registry")
	}

	if _, enumErr := target.Enumerate(context.Background(), client); enumErr == nil {
		t.Error("expected error from Enumerate with unreachable server, got nil")
	}
}
