// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
)

func TestFormResource_lifecycle_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	const addr = "datahub_form.test"
	const wantURN = "urn:li:form:tf-acc-form-pii"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.FormCheckDestroy,
		Steps: []resource.TestStep{
			{
				// Create: explicit id, one prompt, dynamic assignment, default type.
				Config: `
provider "datahub" {}

resource "datahub_form" "test" {
  form_id     = "tf-acc-form-pii"
  name        = "TF Acc Form - PII"
  description = "Collects PII classification"

  prompts = [{
    id                      = "tf-acc-form-pii-classification"
    title                   = "Classify this dataset"
    type                    = "STRUCTURED_PROPERTY"
    structured_property_urn = "urn:li:structuredProperty:tf-acc-classification"
    required                = true
  }]

  dynamic_assignment = {
    or_filters = [{
      and = [{
        field  = "platform.keyword"
        values = ["urn:li:dataPlatform:snowflake"]
      }]
    }]
  }
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(wantURN)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("type"), knownvalue.StringExact("COMPLETION")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("prompts").AtSliceIndex(0).AtMapKey("required"),
						knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(addr,
						tfjsonpath.New("dynamic_assignment").AtMapKey("or_filters").AtSliceIndex(0).
							AtMapKey("and").AtSliceIndex(0).AtMapKey("condition"),
						knownvalue.StringExact("EQUAL")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("actors"), knownvalue.Null()),
				},
			},
			{
				// Update: flip to VERIFICATION, replace the prompts list, set
				// non-default actors, and remove the dynamic assignment.
				Config: `
provider "datahub" {}

resource "datahub_form" "test" {
  form_id     = "tf-acc-form-pii"
  name        = "TF Acc Form - PII v2"
  description = "Collects PII classification and retention"
  type        = "VERIFICATION"

  prompts = [{
    id                      = "tf-acc-form-pii-retention"
    title                   = "Set the retention period"
    type                    = "STRUCTURED_PROPERTY"
    structured_property_urn = "urn:li:structuredProperty:tf-acc-retention"
  }]

  actors = {
    owners = false
    users  = ["urn:li:corpuser:jdoe"]
  }
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("type"), knownvalue.StringExact("VERIFICATION")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("prompts"), knownvalue.ListSizeExact(1)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("prompts").AtSliceIndex(0).AtMapKey("id"),
						knownvalue.StringExact("tf-acc-form-pii-retention")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("prompts").AtSliceIndex(0).AtMapKey("required"),
						knownvalue.Bool(false)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("dynamic_assignment"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("actors").AtMapKey("owners"), knownvalue.Bool(false)),
				},
			},
			{
				// Re-add the assignment so the import step exercises the full shape.
				Config: `
provider "datahub" {}

resource "datahub_form" "test" {
  form_id     = "tf-acc-form-pii"
  name        = "TF Acc Form - PII v2"
  description = "Collects PII classification and retention"
  type        = "VERIFICATION"

  prompts = [{
    id                      = "tf-acc-form-pii-retention"
    title                   = "Set the retention period"
    type                    = "STRUCTURED_PROPERTY"
    structured_property_urn = "urn:li:structuredProperty:tf-acc-retention"
  }]

  actors = {
    owners = false
    users  = ["urn:li:corpuser:jdoe"]
  }

  dynamic_assignment = {
    or_filters = [{
      and = [{
        field  = "domains.keyword"
        values = ["urn:li:domain:tf-acc-finance"]
      }]
    }]
  }
}
`,
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestFormResource_requiredFieldPrompt_rejected verifies the plan-time guard
// mirroring the DataHub SDK: field-level prompts cannot be required.
func TestFormResource_requiredFieldPrompt_rejected(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "datahub_form" "test" {
  name = "TF Acc Form - Invalid"

  prompts = [{
    id                      = "tf-acc-form-invalid-field-prompt"
    title                   = "Classify each column"
    type                    = "FIELDS_STRUCTURED_PROPERTY"
    structured_property_urn = "urn:li:structuredProperty:tf-acc-classification"
    required                = true
  }]
}
`,
				ExpectError: regexp.MustCompile(`Field-level prompts cannot be required`),
			},
		},
	})
}

// TestFormResource_unknownNested_mock feeds nested attribute values from
// another resource's computed output. A literal config resolves schema
// defaults at plan time and never produces unknown, so this is the only step
// that proves a module- or variable-fed config survives plan. PlanOnly: the
// assertion is that the configuration is accepted and produces a plan.
func TestFormResource_unknownNested_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "terraform_data" "seed" {
  input = "urn:li:structuredProperty:tf-acc-classification"
}

resource "datahub_form" "test" {
  name = "TF Acc Form - Unknown"

  prompts = [{
    id                      = "tf-acc-form-unknown-prompt"
    title                   = "Classify this dataset"
    type                    = "STRUCTURED_PROPERTY"
    structured_property_urn = terraform_data.seed.output
  }]

  dynamic_assignment = {
    or_filters = [{
      and = [{
        field  = "platform.keyword"
        values = [terraform_data.seed.output]
      }]
    }]
  }
}
`,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// TestFormResource_recreateOnDrift_mock verifies that when a form is deleted
// out-of-band, Read detects the 404 and removes it from state, so the next
// plan is non-empty (a recreate).
func TestFormResource_recreateOnDrift_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	const cfg = `
provider "datahub" {}

resource "datahub_form" "test" {
  form_id = "tf-acc-form-drift"
  name    = "TF Acc Form - Drift"
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					req, _ := http.NewRequestWithContext(t.Context(), http.MethodDelete,
						server.URL+"/openapi/v3/entity/form/urn:li:form:tf-acc-form-drift", nil)
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

// TestFormsDataSource_mock exercises the plural data source.
func TestFormsDataSource_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	const dsAddr = "data.datahub_forms.all"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "datahub_form" "test" {
  form_id = "tf-acc-form-list"
  name    = "TF Acc Form - List"
}

data "datahub_forms" "all" {
  depends_on = [datahub_form.test]
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(dsAddr, tfjsonpath.New("urns"),
						knownvalue.ListExact([]knownvalue.Check{knownvalue.StringExact("urn:li:form:tf-acc-form-list")})),
				},
			},
		},
	})
}

// TestAcc_Form_Lifecycle validates the resource end-to-end against a live
// DataHub (OSS or Cloud): a structured property feeding a prompt, a dynamic
// assignment, an in-place update that drops the assignment, and an import.
func TestAcc_Form_Lifecycle(t *testing.T) {
	datahubtesting.SetupTarget(t) // configures the live target or skips

	const addr = "datahub_form.test"
	const wantURN = "urn:li:form:tf-acc-live-form-pii"

	cfg := func(withAssignment bool) string {
		assignment := ""
		if withAssignment {
			assignment = `
  dynamic_assignment = {
    or_filters = [{
      and = [{
        field  = "platform.keyword"
        values = ["urn:li:dataPlatform:snowflake"]
      }]
    }]
  }
`
		}
		return `
provider "datahub" {}

resource "datahub_structured_property" "classification" {
  property_id  = "tf-acc-live-form-classification"
  value_type   = "string"
  entity_types = ["dataset"]
}

resource "datahub_form" "test" {
  form_id     = "tf-acc-live-form-pii"
  name        = "TF Acc Live Form - PII"
  description = "Live acceptance form"
  type        = "VERIFICATION"

  prompts = [{
    id                      = "tf-acc-live-form-pii-classification"
    title                   = "Classify this dataset"
    type                    = "STRUCTURED_PROPERTY"
    structured_property_urn = datahub_structured_property.classification.urn
    required                = true
  }]
` + assignment + `
}
`
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.FormCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: cfg(true),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"), knownvalue.StringExact(wantURN)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("type"), knownvalue.StringExact("VERIFICATION")),
				},
			},
			{
				// Drop the dynamic assignment in place.
				Config: cfg(false),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("dynamic_assignment"), knownvalue.Null()),
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
