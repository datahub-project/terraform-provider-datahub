// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
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
				// Update: flip to VERIFICATION, replace the prompts list (now
				// carrying a description), set non-default actors including a
				// group, and remove the dynamic assignment. The post-apply
				// refresh+plan the framework runs is what proves description
				// and groups survive the write/read round trip.
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
    description             = "Retention period in days"
    type                    = "STRUCTURED_PROPERTY"
    structured_property_urn = "urn:li:structuredProperty:tf-acc-retention"
  }]

  actors = {
    owners = false
    users  = ["urn:li:corpuser:jdoe"]
    groups = ["urn:li:corpGroup:tf-acc-governance"]
  }
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("type"), knownvalue.StringExact("VERIFICATION")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("prompts"), knownvalue.ListSizeExact(1)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("prompts").AtSliceIndex(0).AtMapKey("id"),
						knownvalue.StringExact("tf-acc-form-pii-retention")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("prompts").AtSliceIndex(0).AtMapKey("description"),
						knownvalue.StringExact("Retention period in days")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("prompts").AtSliceIndex(0).AtMapKey("required"),
						knownvalue.Bool(false)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("dynamic_assignment"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("actors").AtMapKey("owners"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("actors").AtMapKey("groups"),
						knownvalue.ListExact([]knownvalue.Check{knownvalue.StringExact("urn:li:corpGroup:tf-acc-governance")})),
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
    description             = "Retention period in days"
    type                    = "STRUCTURED_PROPERTY"
    structured_property_urn = "urn:li:structuredProperty:tf-acc-retention"
  }]

  actors = {
    owners = false
    users  = ["urn:li:corpuser:jdoe"]
    groups = ["urn:li:corpGroup:tf-acc-governance"]
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

// TestFormResource_derivedID_mock creates a form without form_id and pins the
// derived id to a literal. The id feeds a RequiresReplace attribute: if the
// derivation ever changes (sanitisation, hash input, truncation), every
// existing state whose form_id was derived plans a destroy-and-recreate --
// this test turns that silent break into a build failure. The step's implicit
// post-apply plan check also proves the derivation is stable across
// plan/apply.
func TestFormResource_derivedID_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	const addr = "datahub_form.test"
	// uid.DeriveID("TF Acc Form - Derived", ..., 48): sanitised name plus a
	// 12-char base32 SHA-256 suffix. Deliberately hardcoded.
	const wantID = "tf-acc-form-derived-ntvz3twhdxcr"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.FormCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "datahub_form" "test" {
  name = "TF Acc Form - Derived"
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("form_id"), knownvalue.StringExact(wantID)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact(wantID)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("urn"),
						knownvalue.StringExact("urn:li:form:"+wantID)),
				},
			},
		},
	})
}

// TestFormResource_sdkShapedImport_mock imports a form the provider did not
// write: seeded with the raw aspect shape the Python SDK or UI leaves behind,
// where `type` and the filter `condition` are absent and actors is the
// materialised PDL default. Importing a provider-written form cannot catch a
// broken read-path default -- the provider's own write path always sends those
// fields. The follow-up step then proves the imported state converges with a
// minimal config: an empty plan is the whole point of the prior-state-aware
// normalisation, and any regression in the type/condition defaults or the
// actors/prompts null suppression surfaces here as a perpetual diff.
func TestFormResource_sdkShapedImport_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	const addr = "datahub_form.imported"
	const formID = "tf-acc-form-sdk"

	const cfg = `
provider "datahub" {}

resource "datahub_form" "imported" {
  form_id = "tf-acc-form-sdk"
  name    = "TF Acc Form - SDK"

  dynamic_assignment = {
    or_filters = [{
      and = [{
        field  = "platform.keyword"
        values = ["urn:li:dataPlatform:snowflake"]
      }]
    }]
  }
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.FormCheckDestroy,
		Steps: []resource.TestStep{
			{
				PreConfig: func() {
					datahubtesting.SeedRawForm(os.Getenv("DATAHUB_GMS_URL"),
						`{"id":"tf-acc-form-sdk","name":"TF Acc Form - SDK",`+
							`"orFilters":[[{"field":"platform.keyword","values":["urn:li:dataPlatform:snowflake"]}]]}`)
				},
				Config:       cfg,
				ResourceName: addr,
				ImportState:  true,
				// The full-URN form of the import id, per the documented
				// contract (the lifecycle test imports by bare form_id).
				ImportStateId:      datahub.FormURNPrefix + formID,
				ImportStatePersist: true,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 imported instance, got %d", len(states))
					}
					attrs := states[0].Attributes
					// The aspect omitted type; state must carry the default,
					// not an empty string that would diff forever.
					want := map[string]string{
						"form_id": formID,
						"type":    "COMPLETION",
						"dynamic_assignment.or_filters.0.and.0.condition": "EQUAL",
						"dynamic_assignment.or_filters.0.and.0.negated":   "false",
					}
					for k, v := range want {
						if got := attrs[k]; got != v {
							return fmt.Errorf("imported attribute %s = %q, want %q", k, got, v)
						}
					}
					// The server-materialised defaults must be suppressed:
					// the config never declared actors or prompts.
					for _, k := range []string{"actors.owners", "prompts.#"} {
						if got, ok := attrs[k]; ok && got != "" {
							return fmt.Errorf("imported attribute %s = %q, want absent", k, got)
						}
					}
					return nil
				},
			},
			{
				// The minimal config must plan empty against the imported
				// state: no diff on type, condition, actors or prompts.
				Config: cfg,
			},
		},
	})
}

// TestFormResource_assignmentWriteFailure_mock drives the two-phase Create
// down its partial-failure path: the formInfo write succeeds, the assignment
// mutation is rejected. The diagnostic must say the form WAS written -- that
// context is the only signal the user has that an unmanaged form now exists
// on the server (Terraform records no state for a failed create).
func TestFormResource_assignmentWriteFailure_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// An empty filter field passes the schema (required only means
				// present) and is rejected server-side, after the form write.
				Config: `
provider "datahub" {}

resource "datahub_form" "test" {
  form_id = "tf-acc-form-partial"
  name    = "TF Acc Form - Partial"

  dynamic_assignment = {
    or_filters = [{
      and = [{
        field  = ""
        values = ["urn:li:dataPlatform:snowflake"]
      }]
    }]
  }
}
`,
				ExpectError: regexp.MustCompile(`form written but setting the dynamic assignment failed`),
			},
		},
	})
}

// TestFormResource_unknownGuardFields_mock feeds the two attributes the
// ValidateConfig guard inspects -- prompt type and required -- from another
// resource's computed output, so both are unknown at validate time. The guard
// must skip unknowns: a rewrite that treats unknown as worst-case (rejecting
// the config) would break every module- or variable-fed prompt list, the
// exact class of bug that shipped twice before in nested-attribute resources.
// PlanOnly: the assertion is that a plan is produced at all.
func TestFormResource_unknownGuardFields_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "terraform_data" "prompt_type" {
  input = "FIELDS_STRUCTURED_PROPERTY"
}

resource "terraform_data" "prompt_required" {
  input = true
}

resource "datahub_form" "test" {
  name = "TF Acc Form - Unknown Guard"

  prompts = [{
    id                      = "tf-acc-form-unknown-guard-prompt"
    title                   = "Classify each column"
    type                    = terraform_data.prompt_type.output
    structured_property_urn = "urn:li:structuredProperty:tf-acc-classification"
    required                = terraform_data.prompt_required.output
  }]
}
`,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// TestFormResource_explicitEmptyUsers_mock pins the documented residual edge:
// an explicit `users = []` does not converge, because empty and absent are
// indistinguishable server-side and the read path returns null. The docs tell
// users to omit the attribute instead. If this test starts failing with an
// unexpectedly EMPTY plan, the normalisation now preserves empty lists --
// update the resource documentation and the PR notes in the same change.
func TestFormResource_explicitEmptyUsers_mock(t *testing.T) {
	server := datahubtesting.NewServer(t)
	t.Setenv("DATAHUB_GMS_URL", server.URL)
	t.Setenv("DATAHUB_GMS_TOKEN", "test-token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             datahubtesting.FormCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: `
provider "datahub" {}

resource "datahub_form" "test" {
  form_id = "tf-acc-form-empty-users"
  name    = "TF Acc Form - Empty Users"

  actors = {
    owners = true
    users  = []
  }
}
`,
				// The refresh reads users back as null; the config's [] then
				// diffs against it on every plan.
				ExpectNonEmptyPlan: true,
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
