// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// mockDataset is an arbitrary dataset URN; the mock server does not validate it.
const mockDataset = "urn:li:dataset:(urn:li:dataPlatform:postgres,tf_example.public.orders,PROD)"

func mustContractURN(t *testing.T, datasetURN string) string {
	t.Helper()
	id, err := datahub.DataContractID(datasetURN)
	if err != nil {
		t.Fatalf("DataContractID: %v", err)
	}
	return datahub.DataContractURNPrefix + id
}

func TestDataContractResource_lifecycle_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	const addr = "datahub_data_contract.test"
	wantURN := mustContractURN(t, mockDataset)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.DataContractCheckDestroy,
		Steps: []resource.TestStep{
			{
				// Create: one data-quality assertion, default state ACTIVE, derived URN.
				Config: fmt.Sprintf(`
provider "datahub" {}

resource "datahub_data_contract" "test" {
  dataset_urn                 = %q
  data_quality_assertion_urns = ["urn:li:assertion:tf-example-dq"]
}
`, mockDataset),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(wantURN)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("state"), knownvalue.StringExact("ACTIVE")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("data_quality_assertion_urns"),
						knownvalue.ListExact([]knownvalue.Check{knownvalue.StringExact("urn:li:assertion:tf-example-dq")})),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("freshness_assertion_urns"), knownvalue.Null()),
				},
			},
			{
				// Update: add a freshness assertion and flip state to PENDING.
				Config: fmt.Sprintf(`
provider "datahub" {}

resource "datahub_data_contract" "test" {
  dataset_urn                 = %q
  state                       = "PENDING"
  freshness_assertion_urns    = ["urn:li:assertion:tf-example-fresh"]
  data_quality_assertion_urns = ["urn:li:assertion:tf-example-dq"]
}
`, mockDataset),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("state"), knownvalue.StringExact("PENDING")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("freshness_assertion_urns"),
						knownvalue.ListExact([]knownvalue.Check{knownvalue.StringExact("urn:li:assertion:tf-example-fresh")})),
				},
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAcc_DataContract_Lifecycle validates the resource end-to-end against a
// live DataHub (OSS or Cloud). A data contract requires a pre-existing dataset,
// so the test is gated on DATAHUB_TEST_DATASET_URN (an existing dataset on the
// target instance). It creates an OSS-compatible custom assertion on that
// dataset and bundles it into a contract, then imports and destroys.
func TestAcc_DataContract_Lifecycle(t *testing.T) {
	datahubtesting.SetupTarget(t) // configures the live target or skips

	datasetURN := os.Getenv("DATAHUB_TEST_DATASET_URN")
	if datasetURN == "" {
		t.Skip("set DATAHUB_TEST_DATASET_URN to an existing dataset URN to run this test")
	}

	const addr = "datahub_data_contract.test"
	wantURN := mustContractURN(t, datasetURN)

	cfg := fmt.Sprintf(`
provider "datahub" {}

resource "datahub_custom_assertion" "dq" {
  entity_urn     = %q
  assertion_type = "Data Contract Check"
  description    = "TF Example - data contract DQ check"
  platform_urn   = "urn:li:dataPlatform:great-expectations"
}

resource "datahub_data_contract" "test" {
  dataset_urn                 = %q
  data_quality_assertion_urns = [datahub_custom_assertion.dq.urn]
}
`, datasetURN, datasetURN)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.DataContractCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(wantURN)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("state"), knownvalue.StringExact("ACTIVE")),
				},
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestDataContractsDataSource_mock exercises the plural data source.
func TestDataContractsDataSource_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	const dsAddr = "data.datahub_data_contracts.all"
	wantURN := mustContractURN(t, mockDataset)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "datahub" {}

resource "datahub_data_contract" "test" {
  dataset_urn                 = %q
  data_quality_assertion_urns = ["urn:li:assertion:tf-example-dq"]
}

data "datahub_data_contracts" "all" {
  depends_on = [datahub_data_contract.test]
}
`, mockDataset),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(dsAddr, tfjsonpath.New("urns"),
						knownvalue.ListExact([]knownvalue.Check{knownvalue.StringExact(wantURN)})),
				},
			},
		},
	})
}
