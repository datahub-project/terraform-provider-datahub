// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/tools/uid"
)

func TestMetadataTestResource_lifecycle_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.MetadataTestCheckDestroy,
		Steps:                    datahubtesting.MetadataTestLifecycleSteps(),
	})
}

func TestAcc_MetadataTest_Lifecycle(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.MetadataTestCheckDestroy,
		Steps:                    datahubtesting.MetadataTestLifecycleSteps(),
	})
}

// TestAcc_MetadataTest_UnknownDefinition feeds definition_json from another
// resource's computed output, so it is unknown during the create plan. A
// literal config can never produce unknown, which is how this bug class
// shipped before: the whole suite passes while every module consumer breaks.
func TestAcc_MetadataTest_UnknownDefinition(t *testing.T) {
	datahubtesting.SetupTarget(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    datahubtesting.MetadataTestUnknownDefinitionSteps(),
	})
}

// TestAcc_MetadataTest_InvalidDefinitionRejected verifies that a definition
// the server's test engine rejects fails the apply with the server's
// validation messages. The mock simulates DataHub Cloud, whose createTest
// validates the definition before writing; OSS accepts anything, so this
// scenario is mock/Cloud-only.
func TestAcc_MetadataTest_InvalidDefinitionRejected(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireCloud(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "datahub_metadata_test" "invalid" {
  test_id         = "tf-example-invalid-definition"
  name            = "TF Example - Invalid Definition"
  category        = "Governance"
  definition_json = jsonencode({ nothing = "here" })
}
`,
				ExpectError: regexp.MustCompile(`Failed to validate test definition`),
			},
		},
	})
}

// TestMetadataTestResource_semanticEquality_mock pins both directions of the
// definition_json JSON semantic equality on the read path, which is where the
// framework applies it (a reformatted config, by contrast, plans a harmless
// one-time in-place update). When the server returns the stored definition
// reformatted -- key order and whitespace changed but the same document --
// refresh must keep the prior state value, or every plan shows a spurious
// diff forever; dropping jsontypes.Normalized from the schema or model causes
// exactly that. And when the definition genuinely changed out-of-band,
// refresh must adopt it so the next plan shows the drift -- an over-broad
// equality would silently stop detecting it.
func TestMetadataTestResource_semanticEquality_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.MetadataTestCheckDestroy,
		Steps: []resource.TestStep{
			{
				// Pretty-printed, keys deliberately not in sorted order, so
				// the mock's reformat (sorted, compact) differs textually.
				Config: `
provider "datahub" {}

resource "datahub_metadata_test" "semeq" {
  test_id  = "tf-example-semantic-equality"
  name     = "TF Example - Semantic Equality"
  category = "Governance"
  definition_json = <<-EOT
    {
      "rules": {
        "property": "ownership.owners.owner",
        "operator": "exists"
      },
      "on": {"types": ["dataset"]}
    }
  EOT
}
`,
			},
			{
				// Make reads return the definition re-marshalled (equivalent
				// JSON, different shape), then refresh: the plan must stay
				// empty, i.e. semantic equality kept the prior state value.
				PreConfig: func() {
					req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
						server.URL+"/test-control/reformat-test-definitions", nil)
					if err != nil {
						t.Fatalf("building reformat toggle request: %v", err)
					}
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Fatalf("enabling reformat-on-read: %v", err)
					}
					_ = resp.Body.Close()
				},
				RefreshState: true,
			},
			{
				// A genuine out-of-band change must still surface as drift.
				PreConfig: func() {
					client, err := datahub.NewClient(server.URL, "test-token")
					if err != nil {
						t.Fatalf("out-of-band client: %v", err)
					}
					err = client.UpdateMetadataTest(context.Background(), "tf-example-semantic-equality",
						datahub.MetadataTestInput{
							Name:           "TF Example - Semantic Equality",
							Category:       "Governance",
							DefinitionJSON: `{"on":{"types":["dataset"]},"rules":{"property":"ownership.owners.owner","operator":"is_false"}}`,
						})
					if err != nil {
						t.Fatalf("out-of-band update failed: %v", err)
					}
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// TestMetadataTestResource_derivedID_mock exercises the omitted-test_id path:
// the provider must derive a deterministic <sanitized-name>-<hash> id
// client-side, because createTest without an id lets the server mint a random
// UUID -- exactly the non-determinism the resource documents itself as
// preventing. The exact-value check fails if the derived id ever reaches the
// server empty (the mock then answers with a recognizable marker id), and the
// PlanOnly step proves the derived id is stable across plans.
func TestMetadataTestResource_derivedID_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	const name = "TF Example - Derived ID"
	wantID := uid.DeriveID(name, []byte(name), 48)
	cfg := `
provider "datahub" {}

resource "datahub_metadata_test" "derived" {
  name     = "` + name + `"
  category = "Governance"
  definition_json = jsonencode({
    on    = { types = ["dataset"] }
    rules = { property = "ownership.owners.owner", operator = "exists" }
  })
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.MetadataTestCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("datahub_metadata_test.derived",
						tfjsonpath.New("test_id"), knownvalue.StringExact(wantID)),
					statecheck.ExpectKnownValue("datahub_metadata_test.derived",
						tfjsonpath.New("urn"), knownvalue.StringExact("urn:li:test:"+wantID)),
				},
			},
			{
				Config:   cfg,
				PlanOnly: true,
			},
		},
	})
}

// TestMetadataTestResource_duplicateID_mock verifies the friendly diagnostic
// when a create collides with an existing test id: the user must be told to
// import (or rename), not shown a bare API error. This guards the
// string-match on the server's "already exists" message in Create.
func TestMetadataTestResource_duplicateID_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	const first = `
provider "datahub" {}

resource "datahub_metadata_test" "a" {
  test_id  = "tf-example-duplicate-id"
  name     = "TF Example - Duplicate A"
  category = "Governance"
  definition_json = jsonencode({
    on    = { types = ["dataset"] }
    rules = { property = "ownership.owners.owner", operator = "exists" }
  })
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.MetadataTestCheckDestroy,
		Steps: []resource.TestStep{
			{Config: first},
			{
				Config: first + `
resource "datahub_metadata_test" "b" {
  test_id  = "tf-example-duplicate-id"
  name     = "TF Example - Duplicate B"
  category = "Governance"
  definition_json = jsonencode({
    on    = { types = ["dataset"] }
    rules = { property = "ownership.owners.owner", operator = "exists" }
  })
}
`,
				ExpectError: regexp.MustCompile(`(?s)Metadata test already exists.*terraform import`),
			},
		},
	})
}

// TestMetadataTestResource_recreateOnDrift_mock verifies that when a test is
// deleted out-of-band, Read observes the 404 and removes it from state, so
// the next plan is a recreate. If Read mishandled the absent entity, the
// drift would go undetected until a failing apply somewhere downstream.
func TestMetadataTestResource_recreateOnDrift_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "datahub_metadata_test" "drift" {
  test_id  = "tf-example-drift"
  name     = "TF Example - Drift"
  category = "Governance"
  definition_json = jsonencode({
    on    = { types = ["dataset"] }
    rules = { property = "ownership.owners.owner", operator = "exists" }
  })
}
`,
			},
			{
				// Delete the test directly on the mock, then refresh: the
				// provider must drop it from state and plan a recreate.
				PreConfig: func() {
					client, err := datahub.NewClient(server.URL, "test-token")
					if err != nil {
						t.Fatalf("out-of-band client: %v", err)
					}
					if err := client.DeleteMetadataTest(context.Background(), "tf-example-drift"); err != nil {
						t.Fatalf("out-of-band delete failed: %v", err)
					}
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// TestMetadataTestResource_rejectedUpdateKeepsState_mock verifies that a
// server-rejected update surfaces the validation messages and leaves the
// prior state intact. If Update committed the plan to state despite the
// error, Terraform would report the rejected definition as applied while the
// server keeps the old one -- divergence with no diagnostic anywhere.
func TestMetadataTestResource_rejectedUpdateKeepsState_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	const header = `
provider "datahub" {}

resource "datahub_metadata_test" "upd" {
  test_id  = "tf-example-rejected-update"
  name     = "TF Example - Rejected Update"
  category = "Governance"
`
	valid := header + `
  definition_json = jsonencode({
    on    = { types = ["dataset"] }
    rules = { property = "ownership.owners.owner", operator = "exists" }
  })
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.MetadataTestCheckDestroy,
		Steps: []resource.TestStep{
			{Config: valid},
			{
				// The mock (simulating Cloud) rejects a definition without
				// the required top-level blocks.
				Config: header + `
  definition_json = jsonencode({ nothing = "here" })
}
`,
				ExpectError: regexp.MustCompile(`Failed to validate test definition`),
			},
			{
				// The original config must plan clean: the failed update may
				// not have leaked the rejected definition into state.
				Config:   valid,
				PlanOnly: true,
			},
		},
	})
}

// TestMetadataTestValidationDataSource_ossCloudRequired_mock verifies the
// full OSS chain for the Cloud-only validation data source: graphql-java's
// FieldUndefined error -> the client's sentinel error -> the "DataHub Cloud
// Required" diagnostic. The shared mock simulates Cloud, so this uses a
// minimal OSS-shaped server. The link a unit test cannot see is the errors.Is
// mapping in the data source: if the client ever wrapped the error
// differently, an OSS user would get raw GraphQL noise instead of being told
// the feature needs Cloud.
func TestMetadataTestValidationDataSource_ossCloudRequired_mock(t *testing.T) {
	oss := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/graphql" {
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			if strings.Contains(string(body), "validateTest") {
				// What OSS graphql-java answers for a query field absent
				// from the schema.
				_, _ = w.Write([]byte(`{"errors":[{"message":` +
					`"Validation error (FieldUndefined@[validateTest]) : Field 'validateTest' in type 'Query' is undefined"}]}`))
				return
			}
			// Provider Configure verifies credentials with the me query.
			_, _ = w.Write([]byte(`{"data":{"me":{"corpUser":` +
				`{"urn":"urn:li:corpuser:datahub","username":"datahub","type":"CORP_USER"}}}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer oss.Close()
	t.Setenv("DATAHUB_GMS_URL", oss.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

data "datahub_metadata_test_validation" "oss" {
  definition_json = jsonencode({
    on    = { types = ["dataset"] }
    rules = { property = "ownership.owners.owner", operator = "exists" }
  })
}
`,
				ExpectError: regexp.MustCompile(`DataHub Cloud Required`),
			},
		},
	})
}

func TestMetadataTestsDataSource_list_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.MetadataTestCheckDestroy,
		Steps:                    datahubtesting.MetadataTestsListSteps("tf-example-list-test"),
	})
}

func TestAcc_MetadataTestsDataSource_List(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if tg.IsLive() {
		t.Skip("list data source test uses exact-match knownvalue check; live targets may have extra pre-existing resources")
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.MetadataTestCheckDestroy,
		Steps:                    datahubtesting.MetadataTestsListSteps("tf-example-list-test"),
	})
}

func TestMetadataTestValidationDataSource_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.MetadataTestCheckDestroy,
		Steps:                    datahubtesting.MetadataTestValidationDataSourceSteps(),
	})
}

func TestAcc_MetadataTestValidationDataSource(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	tg.RequireCloud(t) // Cloud-only data source; skips on live OSS targets

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.MetadataTestCheckDestroy,
		Steps:                    datahubtesting.MetadataTestValidationDataSourceSteps(),
	})
}

// TestMetadataTestValidationDataSource_invalid_mock verifies that a
// syntactically-valid JSON document the server's engine rejects fails the
// plan with the server's messages surfaced in the diagnostic.
func TestMetadataTestValidationDataSource_invalid_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

data "datahub_metadata_test_validation" "invalid" {
  definition_json = jsonencode({ nothing = "here" })
}
`,
				ExpectError: regexp.MustCompile(`Invalid metadata test definition`),
			},
		},
	})
}
