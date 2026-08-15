// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

const mockMetadataTestURNPrefix = "urn:li:test:"

// mockMetadataTest stores the in-memory state for one metadata test.
type mockMetadataTest struct {
	ID             string
	Name           string
	Category       string
	Description    string
	DefinitionJSON string // stored verbatim (the server stores the string as sent)
}

// validateMockTestDefinition mirrors the shape checks the DataHub Cloud test
// engine's definition parser applies (the mock simulates Cloud): the document
// must be JSON, and must carry the top-level `on` and `rules` fields the
// parser requires. It returns nil when valid, or the validation messages.
func validateMockTestDefinition(definitionJSON string) []string {
	var doc map[string]any
	if err := json.Unmarshal([]byte(definitionJSON), &doc); err != nil {
		return []string{fmt.Sprintf("Failed to parse test definition as JSON: %s", err)}
	}
	var messages []string
	if _, ok := doc["on"]; !ok {
		messages = append(messages, "Test definition is missing required field 'on'")
	}
	if _, ok := doc["rules"]; !ok {
		messages = append(messages, "Test definition is missing required field 'rules'")
	}
	return messages
}

// graphQLError writes a GraphQL-style errors response.
func graphQLError(w http.ResponseWriter, msg string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]any{{"message": msg}},
	})
}

// handleCreateTest handles the createTest GraphQL mutation. Mirrors the
// server: non-null name/category/definition (SDL), duplicate-id rejection,
// and (Cloud) definition validation before the write.
func (s *mockServer) handleCreateTest(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	name, _ := input["name"].(string)
	category, _ := input["category"].(string)
	description, _ := input["description"].(string)
	definition, _ := input["definition"].(map[string]any)
	definitionJSON, _ := definition["json"].(string)
	if name == "" || category == "" || definition == nil {
		graphQLError(w, "Validation error: NullValueForNonNullArgument in CreateTestInput")
		return
	}
	if msgs := validateMockTestDefinition(definitionJSON); len(msgs) > 0 {
		graphQLError(w, "Failed to validate test definition: \n"+strings.Join(msgs, "\n"))
		return
	}

	id, _ := input["id"].(string)
	if id == "" {
		// The real server mints a random UUID here; the provider never relies
		// on this path, so a recognizable marker is more useful than realism.
		id = fmt.Sprintf("mock-generated-%d", len(s.metadataTests)+1)
	}

	s.mu.Lock()
	_, exists := s.metadataTests[id]
	if !exists {
		s.metadataTests[id] = mockMetadataTest{
			ID:             id,
			Name:           name,
			Category:       category,
			Description:    description,
			DefinitionJSON: definitionJSON,
		}
	}
	s.mu.Unlock()

	if exists {
		graphQLError(w, "This Test already exists!")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"createTest": mockMetadataTestURNPrefix + id},
	})
}

// handleUpdateTest handles the updateTest GraphQL mutation. Like the server,
// it rebuilds the whole testInfo aspect from the input (upsert semantics),
// after (Cloud) validating the definition.
func (s *mockServer) handleUpdateTest(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	input, _ := variables["input"].(map[string]any)
	id := strings.TrimPrefix(urn, mockMetadataTestURNPrefix)

	name, _ := input["name"].(string)
	category, _ := input["category"].(string)
	description, _ := input["description"].(string)
	definition, _ := input["definition"].(map[string]any)
	definitionJSON, _ := definition["json"].(string)
	if name == "" || category == "" || definition == nil {
		graphQLError(w, "Validation error: NullValueForNonNullArgument in UpdateTestInput")
		return
	}
	if msgs := validateMockTestDefinition(definitionJSON); len(msgs) > 0 {
		graphQLError(w, "Failed to validate test definition: \n"+strings.Join(msgs, "\n"))
		return
	}

	s.mu.Lock()
	s.metadataTests[id] = mockMetadataTest{
		ID:             id,
		Name:           name,
		Category:       category,
		Description:    description,
		DefinitionJSON: definitionJSON,
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"updateTest": urn},
	})
}

// handleDeleteTest handles the deleteTest GraphQL mutation (hard delete).
func (s *mockServer) handleDeleteTest(w http.ResponseWriter, variables map[string]any) {
	urn, _ := variables["urn"].(string)
	id := strings.TrimPrefix(urn, mockMetadataTestURNPrefix)

	s.mu.Lock()
	delete(s.metadataTests, id)
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"deleteTest": true},
	})
}

// handleListTests handles the listTests GraphQL query.
func (s *mockServer) handleListTests(w http.ResponseWriter, _ map[string]any) {
	s.mu.Lock()
	tests := make([]map[string]any, 0, len(s.metadataTests))
	for id := range s.metadataTests {
		tests = append(tests, map[string]any{"urn": mockMetadataTestURNPrefix + id})
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"listTests": map[string]any{
				"total": len(tests),
				"tests": tests,
			},
		},
	})
}

// handleValidateTest handles the validateTest GraphQL query (Cloud-only; the
// mock simulates Cloud).
func (s *mockServer) handleValidateTest(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	definitionJSON, _ := input["json"].(string)

	msgs := validateMockTestDefinition(definitionJSON)
	result := map[string]any{"isValid": len(msgs) == 0}
	if len(msgs) > 0 {
		result["messages"] = msgs
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"validateTest": result},
	})
}

// handleReformatTestDefinitions toggles reformat-on-read for metadata test
// definitions (see the field comment on mockServer). Called from test
// PreConfig:
//
//	POST /test-control/reformat-test-definitions    (enables reformatting)
//	DELETE /test-control/reformat-test-definitions  (reverts to verbatim)
func (s *mockServer) handleReformatTestDefinitions(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	switch r.Method {
	case http.MethodPost:
		s.reformatTestDefinitions = true
	case http.MethodDelete:
		s.reformatTestDefinitions = false
	default:
		s.mu.Unlock()
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// handleMetadataTestItem handles GET /openapi/v3/entity/test/{urn}. The
// response mimics the Cloud read shape, including the server-computed
// definition.md5 the provider must tolerate and ignore.
func (s *mockServer) handleMetadataTestItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	urn := strings.TrimPrefix(r.URL.Path, "/openapi/v3/entity/test/")
	id := strings.TrimPrefix(urn, mockMetadataTestURNPrefix)

	s.mu.Lock()
	mt, ok := s.metadataTests[id]
	reformat := s.reformatTestDefinitions
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	definitionJSON := mt.DefinitionJSON
	if reformat {
		// Re-marshal through a map: keys come out sorted and whitespace
		// compacted, i.e. equivalent JSON in a different shape.
		var doc any
		if err := json.Unmarshal([]byte(definitionJSON), &doc); err == nil {
			if b, err := json.Marshal(doc); err == nil {
				definitionJSON = string(b)
			}
		}
	}

	infoValue := map[string]any{
		"name":     mt.Name,
		"category": mt.Category,
		"definition": map[string]any{
			"type": "JSON",
			"json": definitionJSON,
			"md5":  "mock-md5-of-definition",
		},
	}
	if mt.Description != "" {
		infoValue["description"] = mt.Description
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"urn":      mockMetadataTestURNPrefix + id,
		"testKey":  map[string]any{"value": map[string]any{"id": id}},
		"testInfo": map[string]any{"value": infoValue},
	})
}

// MetadataTestLifecycleSteps returns test steps for datahub_metadata_test:
// create (deterministic test_id + a definition with on/rules blocks), update
// the description + definition, then import and verify.
func MetadataTestLifecycleSteps() []resource.TestStep {
	const addr = "datahub_metadata_test.test"

	return []resource.TestStep{
		{
			Config: providerBlock + `
resource "datahub_metadata_test" "test" {
  test_id     = "tf-example-dataset-ownership"
  name        = "TF Example - Dataset Ownership"
  category    = "Governance"
  description = "Every dataset must have at least one owner."
  definition_json = jsonencode({
    on = {
      types = ["dataset"]
    }
    rules = {
      property = "ownership.owners.owner"
      operator = "exists"
    }
  })
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact("urn:li:test:tf-example-dataset-ownership")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("test_id"), knownvalue.StringExact("tf-example-dataset-ownership")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact("TF Example - Dataset Ownership")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("category"), knownvalue.StringExact("Governance")),
			},
		},
		{
			Config: providerBlock + `
resource "datahub_metadata_test" "test" {
  test_id     = "tf-example-dataset-ownership"
  name        = "TF Example - Dataset Ownership"
  category    = "Governance"
  description = "Every PROD dataset must have at least one owner."
  definition_json = jsonencode({
    on = {
      types = ["dataset"]
      conditions = {
        property = "origin"
        operator = "equals"
        value    = "PROD"
      }
    }
    rules = {
      property = "ownership.owners.owner"
      operator = "exists"
    }
  })
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("description"), knownvalue.StringExact("Every PROD dataset must have at least one owner.")),
			},
		},
		{
			ResourceName:      addr,
			ImportState:       true,
			ImportStateVerify: true,
		},
	}
}

// MetadataTestCheckDestroy verifies every datahub_metadata_test is removed.
func MetadataTestCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_metadata_test" {
			continue
		}
		id := rs.Primary.Attributes["test_id"]
		if id == "" {
			id = rs.Primary.ID
		}
		info, getErr := client.GetMetadataTestByID(ctx, id)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking metadata test %q: %w", id, getErr)
		}
		if info != nil {
			return stillExistsAfterDestroyError(ctx, client, "datahub_metadata_test",
				datahub.MetadataTestURNPrefix+id)
		}
	}
	return nil
}

// MetadataTestUnknownDefinitionSteps supplies definition_json as a computed
// value (another resource's output), which is unknown during the create plan.
// A jsontypes.Normalized attribute must accept that; a Go-native model field
// could not. PlanOnly with ExpectNonEmptyPlan keeps the scenario at the
// validate/plan boundary and off the server entirely.
func MetadataTestUnknownDefinitionSteps() []resource.TestStep {
	return planOnlyStep(`
provider "datahub" {}

resource "terraform_data" "seed" {
  input = jsonencode({
    on    = { types = ["dataset"] }
    rules = { property = "ownership.owners.owner", operator = "exists" }
  })
}

resource "datahub_metadata_test" "test" {
  test_id         = "tf-example-unknown-definition"
  name            = "TF Example - Unknown Definition"
  category        = "Governance"
  definition_json = terraform_data.seed.output
}
`)
}

// MetadataTestsListSteps returns a test step that creates a metadata test and
// reads the datahub_metadata_tests data source, verifying the test's URN
// appears.
func MetadataTestsListSteps(testID string) []resource.TestStep {
	urn := mockMetadataTestURNPrefix + testID
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_metadata_test" "test" {
  test_id  = %q
  name     = "List DS test"
  category = "Governance"
  definition_json = jsonencode({
    on    = { types = ["dataset"] }
    rules = { property = "ownership.owners.owner", operator = "exists" }
  })
}

data "datahub_metadata_tests" "all" {
  depends_on = [datahub_metadata_test.test]
}
`, testID)

	return []resource.TestStep{
		{
			Config: cfg,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(
					"data.datahub_metadata_tests.all",
					tfjsonpath.New("urns"),
					knownvalue.ListExact([]knownvalue.Check{
						knownvalue.StringExact(urn),
					}),
				),
			},
		},
	}
}

// MetadataTestValidationDataSourceSteps exercises the Cloud-only
// datahub_metadata_test_validation data source: a valid definition passes
// through, and the definition_json output can feed the resource so the
// validation is ordered ahead of the write.
func MetadataTestValidationDataSourceSteps() []resource.TestStep {
	return []resource.TestStep{
		{
			Config: providerBlock + `
data "datahub_metadata_test_validation" "check" {
  definition_json = jsonencode({
    on    = { types = ["dataset"] }
    rules = { property = "glossaryTerms.terms.urn", operator = "exists" }
  })
}

resource "datahub_metadata_test" "validated" {
  test_id         = "tf-example-validated-terms"
  name            = "TF Example - Validated Glossary Terms"
  category        = "Governance"
  definition_json = data.datahub_metadata_test_validation.check.definition_json
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("datahub_metadata_test.validated", tfjsonpath.New("urn"),
					knownvalue.StringExact("urn:li:test:tf-example-validated-terms")),
			},
		},
	}
}
