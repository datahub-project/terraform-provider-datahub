// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"context"
	"testing"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/importtarget"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// TestConnectionImportTarget_Enumerate verifies that the registered
// datahub_connection import target filters out system (__ prefix) and OAuth
// (urn_li_ prefix) connections that the datahub_connection resource cannot model,
// while returning user-managed connections.
func TestConnectionImportTarget_Enumerate(t *testing.T) {
	srv := datahubtesting.NewServer(t)
	client, err := datahub.NewClient(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}
	ctx := context.Background()

	// Seed 1 user-managed connection (UUID format -- importable).
	_, err = client.UpsertConnection(ctx, datahub.UpsertConnectionInput{
		ID:       "4c7cf6d3-5720-443c-bdbf-febd5c7644a8",
		Name:     "prod-databricks",
		Platform: "databricks",
		Blob:     `{}`,
	})
	if err != nil {
		t.Fatalf("seeding user connection: %v", err)
	}

	// Seed 1 system connection (__ prefix) -- must be filtered out.
	_, err = client.UpsertConnection(ctx, datahub.UpsertConnectionInput{
		ID:       "__system_teams-0",
		Name:     "system-teams",
		Platform: "teams",
		Blob:     `{}`,
	})
	if err != nil {
		t.Fatalf("seeding system connection: %v", err)
	}

	// Seed 1 OAuth connection (urn_li_ prefix) -- must be filtered out.
	_, err = client.UpsertConnection(ctx, datahub.UpsertConnectionInput{
		ID:       "urn_li_corpuser_alice_example_com__urn_li_service_abc123",
		Name:     "oauth-connection",
		Platform: "oauth",
		Blob:     `{}`,
	})
	if err != nil {
		t.Fatalf("seeding OAuth connection: %v", err)
	}

	target, ok := importtarget.ByResourceType("datahub_connection")
	if !ok {
		t.Fatal("datahub_connection not registered in importtarget registry")
	}

	urns, err := target.Enumerate(ctx, client)
	if err != nil {
		t.Fatalf("Enumerate: %v", err)
	}

	if len(urns) != 1 {
		t.Errorf("Enumerate returned %d URNs, want 1 (system and OAuth connections must be filtered); got: %v", len(urns), urns)
		return
	}
	const wantURN = "urn:li:dataHubConnection:4c7cf6d3-5720-443c-bdbf-febd5c7644a8"
	if urns[0] != wantURN {
		t.Errorf("Enumerate returned URN %q, want %q", urns[0], wantURN)
	}
}

// TestConnectionImportTarget_EnumerateError verifies that a DataHub API error is
// propagated (with wrapping) from the Enumerate function.
func TestConnectionImportTarget_EnumerateError(t *testing.T) {
	client, err := datahub.NewClient("http://127.0.0.1:1", "test-token")
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}

	target, ok := importtarget.ByResourceType("datahub_connection")
	if !ok {
		t.Fatal("datahub_connection not registered in importtarget registry")
	}

	_, enumErr := target.Enumerate(context.Background(), client)
	if enumErr == nil {
		t.Error("expected error from Enumerate with unreachable server, got nil")
	}
}
