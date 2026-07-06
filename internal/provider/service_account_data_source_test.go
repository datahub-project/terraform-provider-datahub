// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

// TestAcc_ServiceAccountDataSource_Read creates a service account and reads it
// back via both the singular datahub_service_account and bulk
// datahub_service_accounts data sources.
func TestAcc_ServiceAccountDataSource_Read(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	id := tg.Name("tfprovider-sa-ds")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.ServiceAccountCheckDestroy,
		Steps:                    datahubtesting.ServiceAccountDataSourceSteps(id),
	})
}
