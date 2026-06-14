// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// TestVolumeAssertionImportGuard_mock verifies that a direct `terraform import`
// of a non-NATIVE assertion (an ingested EXTERNAL one) into
// datahub_volume_assertion is refused at point-of-import by the resource's
// source guard, so a user cannot accidentally bring an ingestion-owned
// assertion under Terraform management.
func TestVolumeAssertionImportGuard_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.VolumeAssertionCheckDestroy,
		Steps:                    datahubtesting.VolumeAssertionImportGuardSteps(),
	})
}

// TestAssertionEnumeration_excludesNonNative_mock verifies end-to-end (through
// the mock's assertion search) that the monitor-assertion enumerator returns
// only NATIVE assertions and excludes ingested EXTERNAL ones, so an extract
// never proposes importing assertions owned by an ingestion source.
func TestAssertionEnumeration_excludesNonNative_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	datahubtesting.SeedAssertion(server.URL, "urn:li:assertion:vol-native", "VOLUME", "NATIVE", "ROW_COUNT_TOTAL")
	datahubtesting.SeedAssertion(server.URL, "urn:li:assertion:vol-external", "VOLUME", "EXTERNAL", "ROW_COUNT_TOTAL")

	client, err := datahub.NewClient(server.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	urns, err := client.ListVolumeAssertionURNs(context.Background())
	if err != nil {
		t.Fatalf("ListVolumeAssertionURNs: %v", err)
	}
	if len(urns) != 1 || urns[0] != "urn:li:assertion:vol-native" {
		t.Fatalf("ListVolumeAssertionURNs() = %v, want only [urn:li:assertion:vol-native] (EXTERNAL must be excluded)", urns)
	}
}
