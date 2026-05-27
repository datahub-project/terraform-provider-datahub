// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

func TestAcc_SecretsDataSource_List(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("list data source test uses exact-match knownvalue check; live targets may have extra pre-existing resources")
	}
	secretName := tg.Name("tfprovider-list-sec")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_11_0),
		},
		Steps: datahubtesting.SecretsListSteps(secretName),
	})
}
