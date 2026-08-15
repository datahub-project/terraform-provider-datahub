// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
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

// TestDataContractResource_recreateOnDrift_mock verifies that when a contract is
// deleted out-of-band, Read detects the 404 and removes it from state, so the
// next plan is non-empty (a recreate).
func TestDataContractResource_recreateOnDrift_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	wantURN := mustContractURN(t, mockDataset)
	cfg := fmt.Sprintf(`
provider "datahub" {}

resource "datahub_data_contract" "test" {
  dataset_urn                 = %q
  data_quality_assertion_urns = ["urn:li:assertion:tf-example-dq"]
}
`, mockDataset)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				// Delete the contract directly on the mock, then refresh: the
				// provider must drop it from state and plan a recreate.
				PreConfig: func() {
					req, _ := http.NewRequestWithContext(t.Context(), http.MethodDelete,
						server.URL+"/openapi/v3/entity/datacontract/"+wantURN, nil)
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Fatalf("out-of-band delete failed: %v", err)
					}
					_ = resp.Body.Close()
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// contractTestDataset is the dataset the lifecycle test provisions for itself
// (via EnsureDatasetEntity) when DATAHUB_TEST_DATASET_URN is not set. Named
// distinctly from the assertion tests' tf_assertion_test dataset so a parallel
// test's cleanup cannot delete it out from under the contract.
const contractTestDataset = "urn:li:dataset:(urn:li:dataPlatform:sqlite,tf_contract_test.orders,PROD)"

// TestAcc_DataContract_Lifecycle validates the resource end-to-end against a
// live DataHub (OSS or Cloud). A data contract requires the referenced dataset
// to exist server-side: set DATAHUB_TEST_DATASET_URN to use a dataset already
// on the target instance, otherwise the test provisions a minimal dataset
// entity itself and hard-deletes it on cleanup -- which is what lets the test
// run un-gated in the nightly live-acceptance workflow instead of being
// skipped forever (nothing in the repository ever set that variable). It
// creates an OSS-compatible custom assertion on the dataset, bundles it into a
// contract, imports it, enumerates it through the plural data source, and
// destroys.
func TestAcc_DataContract_Lifecycle(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)

	datasetURN := os.Getenv("DATAHUB_TEST_DATASET_URN")
	if datasetURN == "" {
		datasetURN = contractTestDataset
		// No-op on the mock, which does not validate entity existence.
		tg.EnsureDatasetEntity(t, datasetURN)
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
			{
				// Plural data source, against the same target. This is the only
				// live execution of the searchAcrossEntities-backed list query;
				// the mock test below checks exact contents, which a live target
				// with pre-existing contracts cannot promise. The backing index
				// is eventually consistent, so poll until the contract is
				// indexed before asserting membership -- without the wait this
				// step would flake for reasons that are not defects.
				PreConfig: func() { waitForDataContractIndexed(t, wantURN) },
				Config: cfg + `
data "datahub_data_contracts" "all" {
  depends_on = [datahub_data_contract.test]
}
`,
				Check: contractURNInList("data.datahub_data_contracts.all", wantURN),
			},
		},
	})
}

// waitForDataContractIndexed polls ListDataContractURNs until wantURN appears
// or the budget is exhausted. The mock's list is strongly consistent, so this
// returns on the first probe there; on a live target it absorbs the OpenSearch
// indexing lag between the contract's create and its first appearance in
// search results.
func waitForDataContractIndexed(t *testing.T, wantURN string) {
	t.Helper()
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		t.Fatalf("waitForDataContractIndexed: building client: %v", err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		urns, listErr := client.ListDataContractURNs(t.Context())
		if listErr == nil && slices.Contains(urns, wantURN) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("data contract %s not indexed after 60s (last list error: %v)", wantURN, listErr)
		}
		time.Sleep(2 * time.Second)
	}
}

// contractURNInList asserts urn appears somewhere in the urns list attribute of
// the data source at addr. A containment check rather than exact-match: a live
// target may hold contracts this test did not create.
func contractURNInList(addr, urn string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[addr]
		if !ok {
			return fmt.Errorf("data source %s not found in state", addr)
		}
		for k, v := range rs.Primary.Attributes {
			if strings.HasPrefix(k, "urns.") && v == urn {
				return nil
			}
		}
		return fmt.Errorf("URN %q not found in %s.urns", urn, addr)
	}
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
