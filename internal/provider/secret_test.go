// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
)

func TestAcc_Secret_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	name := tg.Name("tfprovider-secret")

	resource.Test(t, resource.TestCase{
		// WriteOnly attribute requires Terraform CLI 1.11+.
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_11_0),
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.SecretLifecycleSteps(name),
	})
}
