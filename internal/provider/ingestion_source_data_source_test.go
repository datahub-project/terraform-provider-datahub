// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

func TestAcc_IngestionSourceDataSource_Read(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	sourceID := tg.Name("tfprovider-source-ds")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.IngestionSourceDataSourceSteps(sourceID),
	})
}
