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

// contractTestDataset returns the dataset the lifecycle test provisions for
// itself (via EnsureDatasetEntity) when DATAHUB_TEST_DATASET_URN is not set.
// Named distinctly from the assertion tests' tf_assertion_test dataset so a
// parallel test's cleanup cannot delete it out from under the contract.
//
// The table name is unique per live run. A data contract's URN is derived
// from its dataset URN, so a fixed dataset gave every run the same contract
// URN, and the list step could then pass on a stale search result that still
// carried the previous run's contract instead of proving this run's contract
// was found. A fresh URN per run can only be found if it was really indexed.
// The mock keeps the stable name.
func contractTestDataset(tg *datahubtesting.Target) string {
	return "urn:li:dataset:(urn:li:dataPlatform:sqlite,tf_contract_test." + tg.Name("orders") + ",PROD)"
}

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
		datasetURN = contractTestDataset(tg)
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

	steps := []resource.TestStep{
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
	}

	// The plural data source cannot work on OSS, and this is not a lag or a
	// defect in the provider. datahub_data_contracts is backed by
	// searchAcrossEntities, and DataContractType -- the loadable GraphQL type
	// that hydrates a search hit into an Entity -- is registered only by the
	// Cloud plugin, not by the OSS engine. The dataContract entity is indexed
	// on both (searchGroup: primary in the entity registry), so on OSS the
	// search finds the document, has nothing to hydrate it with, and returns
	// null for a non-null Entity field. Every call errors; no poll budget can
	// help. Upstream marks the type "will be moved to OSS", so this skip has a
	// removal condition: delete it once DataContractType is registered in the
	// OSS GmsGraphQLEngine.
	//
	// The mock simulates Cloud, so this step still runs there and in Cloud
	// acceptance -- only a live OSS target skips it.
	if tg.IsCloud() {
		steps = append(steps, resource.TestStep{
			// The only live execution of the searchAcrossEntities-backed list
			// query. The mock test below checks exact contents, which a live
			// target carrying pre-existing contracts cannot promise. The index
			// is eventually consistent, so poll before asserting membership --
			// without the wait this would flake for reasons that are not
			// defects.
			PreConfig: func() { waitForDataContractIndexed(t, wantURN) },
			Config: cfg + `
data "datahub_data_contracts" "all" {
  depends_on = [datahub_data_contract.test]
}
`,
			Check: contractURNInList("data.datahub_data_contracts.all", wantURN),
		})
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.DataContractCheckDestroy,
		Steps:                    steps,
	})
}

// dataContractIndexBudget bounds waitForDataContractIndexed. The poll returns
// as soon as the contract appears, so the budget only costs time when the
// contract never shows up. With the list bypassing DataHub's search result
// cache a fresh contract appears within seconds; before that, a poll could
// sit on a cached empty result for minutes, which no budget fixed.
const dataContractIndexBudget = 60 * time.Second

// dataContractStableProbes is how many consecutive probes must list the
// contract before the wait returns. One sighting is not enough on a live
// target: a search served by an index copy that has refreshed can be followed
// by one served by a copy that has not, so the data source read that comes
// next can miss a contract the poll has just seen. Requiring a run of
// sightings a couple of seconds apart lets every copy catch up first.
const dataContractStableProbes = 3

// waitForDataContractIndexed polls ListDataContractURNs until wantURN has been
// listed by dataContractStableProbes consecutive probes, or the budget is
// exhausted. The mock's list is strongly consistent, so this returns after the
// first few probes there; on a live target it absorbs the indexing lag between
// the contract's create and its consistent appearance in search results.
func waitForDataContractIndexed(t *testing.T, wantURN string) {
	t.Helper()
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		t.Fatalf("waitForDataContractIndexed: building client: %v", err)
	}
	// A query that errors every time is not the same as one that has not
	// caught up, and reporting the first as the second sends the reader after
	// indexing lag that was never the cause. Track whether any probe has
	// succeeded: if none has by the deadline, the list call is broken and the
	// message says so.
	start := time.Now()
	deadline := start.Add(dataContractIndexBudget)
	var anySucceeded bool
	var lastErr error
	var seen int
	for {
		urns, listErr := client.ListDataContractURNs(t.Context())
		if listErr == nil {
			anySucceeded = true
			if slices.Contains(urns, wantURN) {
				seen++
				if seen >= dataContractStableProbes {
					t.Logf("data contract %s listed by %d consecutive probes after %s",
						wantURN, seen, time.Since(start).Round(time.Second))
					return
				}
			} else {
				seen = 0
			}
		} else {
			seen = 0
			lastErr = listErr
		}
		if time.Now().After(deadline) {
			if !anySucceeded {
				t.Fatalf("listing data contracts failed on every probe for %s, so %s was never searched for "+
					"-- this is a failing query, not indexing lag: %v", dataContractIndexBudget, wantURN, lastErr)
			}
			t.Fatalf("data contract %s was not listed by %d consecutive probes within %s, though the query itself succeeded",
				wantURN, dataContractStableProbes, dataContractIndexBudget)
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
