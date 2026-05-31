// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_CorpUser_DataSource reads the built-in "datahub" user, which exists on
// both the mock (seeded) and a live OSS Quickstart (the bootstrap admin user).
func TestAcc_CorpUser_DataSource(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.CorpUserDataSourceSteps("datahub"),
	})
}

// TestAcc_CorpUser_NotFound verifies the data source surfaces a clear diagnostic
// when the username does not exist.
func TestAcc_CorpUser_NotFound(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	username := tg.Name("tfprovider-nonexistent-user")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.CorpUserDataSourceNotFoundSteps(username),
	})
}
