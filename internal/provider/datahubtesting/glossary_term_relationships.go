// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"context"
	"fmt"
	"os"
	"regexp"

	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// glossaryTermRelationshipConfig renders two terms and two typed edges on the
// same source term (isA and hasA). Two edges on one source exercises
// coexistence within the single glossaryRelatedTerms aspect, and both edges
// take their URNs as expression inputs, so term_urn/related_term_urn arrive
// unknown at plan time.
func glossaryTermRelationshipConfig(sourceID, relatedID string) string {
	return providerBlock + fmt.Sprintf(`
resource "datahub_glossary_term" "source" {
  term_id = %q
  name    = "Net Revenue"
}

resource "datahub_glossary_term" "related" {
  term_id = %q
  name    = "Revenue Concept"
}

resource "datahub_glossary_term_relationship" "inherits" {
  term_urn          = datahub_glossary_term.source.urn
  relationship_type = "isA"
  related_term_urn  = datahub_glossary_term.related.urn
}

resource "datahub_glossary_term_relationship" "contains" {
  term_urn          = datahub_glossary_term.source.urn
  relationship_type = "hasA"
  related_term_urn  = datahub_glossary_term.related.urn
}
`, sourceID, relatedID)
}

// GlossaryTermRelationshipSteps returns test steps covering create (both
// relationship types on one source term), import by composite ID, and
// out-of-band removal being re-created, for datahub_glossary_term_relationship.
func GlossaryTermRelationshipSteps(sourceID, relatedID string) []resource.TestStep {
	const inheritsAddr = "datahub_glossary_term_relationship.inherits"
	const containsAddr = "datahub_glossary_term_relationship.contains"
	sourceURN := "urn:li:glossaryTerm:" + sourceID
	relatedURN := "urn:li:glossaryTerm:" + relatedID
	cfg := glossaryTermRelationshipConfig(sourceID, relatedID)

	return []resource.TestStep{
		{
			Config: cfg,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(inheritsAddr, tfjsonpath.New("term_urn"), knownvalue.StringExact(sourceURN)),
				statecheck.ExpectKnownValue(inheritsAddr, tfjsonpath.New("relationship_type"), knownvalue.StringExact("isA")),
				statecheck.ExpectKnownValue(inheritsAddr, tfjsonpath.New("related_term_urn"), knownvalue.StringExact(relatedURN)),
				statecheck.ExpectKnownValue(inheritsAddr, tfjsonpath.New("id"), knownvalue.StringExact(sourceURN+"|isA|"+relatedURN)),
				statecheck.ExpectKnownValue(containsAddr, tfjsonpath.New("relationship_type"), knownvalue.StringExact("hasA")),
				statecheck.ExpectKnownValue(containsAddr, tfjsonpath.New("id"), knownvalue.StringExact(sourceURN+"|hasA|"+relatedURN)),
			},
		},
		{
			// Import by composite ID.
			ResourceName:      inheritsAddr,
			ImportState:       true,
			ImportStateVerify: true,
		},
		{
			// Out-of-band removal: Read detects the missing edge and the next
			// apply re-creates it. The sibling hasA edge must survive both the
			// removal and the re-create (edges are owned one per resource).
			PreConfig: func() {
				client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
				if err != nil {
					panic(fmt.Sprintf("GlossaryTermRelationshipSteps PreConfig: %v", err))
				}
				if delErr := client.RemoveRelatedTerm(context.Background(), sourceURN, datahub.TermRelationshipIsA, relatedURN); delErr != nil {
					panic(fmt.Sprintf("GlossaryTermRelationshipSteps PreConfig: remove failed: %v", delErr))
				}
			},
			Config: cfg,
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(inheritsAddr, plancheck.ResourceActionCreate),
					plancheck.ExpectResourceAction(containsAddr, plancheck.ResourceActionNoop),
				},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(inheritsAddr, tfjsonpath.New("id"), knownvalue.StringExact(sourceURN+"|isA|"+relatedURN)),
				statecheck.ExpectKnownValue(containsAddr, tfjsonpath.New("id"), knownvalue.StringExact(sourceURN+"|hasA|"+relatedURN)),
			},
		},
	}
}

// GlossaryTermRelationshipFromVariableSteps drives relationship_type from an
// input variable rather than a literal, per the design doc's "one non-literal
// input per resource" rule: a variable-fed attribute is unknown during the
// validate walk, so this catches any presence check shaped
// !x.IsNull() && x.ValueString() != "" that would misread unknown as absent
// (and any validator that fails to skip unknown).
func GlossaryTermRelationshipFromVariableSteps(sourceID, relatedID string) []resource.TestStep {
	cfg := providerBlock + `
variable "rel_type" {
  type = string
}
` + fmt.Sprintf(`
resource "datahub_glossary_term" "source" {
  term_id = %q
  name    = "Operating Margin"
}

resource "datahub_glossary_term" "related" {
  term_id = %q
  name    = "Margin Concept"
}

resource "datahub_glossary_term_relationship" "test" {
  term_urn          = datahub_glossary_term.source.urn
  relationship_type = var.rel_type
  related_term_urn  = datahub_glossary_term.related.urn
}
`, sourceID, relatedID)

	return []resource.TestStep{
		{
			Config: cfg,
			ConfigVariables: config.Variables{
				"rel_type": config.StringVariable("isA"),
			},
		},
	}
}

// GlossaryTermRelationshipSelfSteps asserts that a self-relationship is
// rejected at validate time, before any API call (so no terms are created).
func GlossaryTermRelationshipSelfSteps(termID string) []resource.TestStep {
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_glossary_term_relationship" "test" {
  term_urn          = "urn:li:glossaryTerm:%s"
  relationship_type = "isA"
  related_term_urn  = "urn:li:glossaryTerm:%s"
}
`, termID, termID)

	return []resource.TestStep{
		{
			Config:      cfg,
			ExpectError: regexp.MustCompile(`Self-relationship not allowed`),
		},
	}
}

// GlossaryTermRelationshipImportErrorSteps verifies ImportState rejects every
// malformed composite-ID shape with a specific diagnostic, and rejects a
// well-formed ID naming an edge that does not exist. Import IDs are hand-typed
// by users, and a parse regression here is state corruption: fields imported
// under the wrong attribute poison the next plan silently. The final step
// re-applies the config so cleanup destroy succeeds.
func GlossaryTermRelationshipImportErrorSteps(sourceID, relatedID string) []resource.TestStep {
	const addr = "datahub_glossary_term_relationship.inherits"
	sourceURN := "urn:li:glossaryTerm:" + sourceID
	relatedURN := "urn:li:glossaryTerm:" + relatedID
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_glossary_term" "source" {
  term_id = %q
  name    = "Import Error Source"
}

resource "datahub_glossary_term" "related" {
  term_id = %q
  name    = "Import Error Related"
}

resource "datahub_glossary_term_relationship" "inherits" {
  term_urn          = datahub_glossary_term.source.urn
  relationship_type = "isA"
  related_term_urn  = datahub_glossary_term.related.urn
}
`, sourceID, relatedID)
	importID := func(id string) resource.ImportStateIdFunc {
		return func(_ *terraform.State) (string, error) { return id, nil }
	}

	return []resource.TestStep{
		{Config: cfg},
		{
			// A bare URN: only one part, not a composite ID.
			ResourceName:      addr,
			ImportState:       true,
			ImportStateIdFunc: importID(sourceURN),
			ExpectError:       regexp.MustCompile(`Invalid import ID`),
		},
		{
			// Right shape, but the source is not a glossaryTerm URN.
			ResourceName:      addr,
			ImportState:       true,
			ImportStateIdFunc: importID("urn:li:tag:pii|isA|" + relatedURN),
			ExpectError:       regexp.MustCompile(`Both\s+term\s+URNs\s+must\s+start`),
		},
		{
			// "contains" is the UI label, not the enum value "hasA".
			ResourceName:      addr,
			ImportState:       true,
			ImportStateIdFunc: importID(sourceURN + "|contains|" + relatedURN),
			ExpectError:       regexp.MustCompile(`not valid`),
		},
		{
			// Well-formed, but only the isA edge exists. Importing a
			// nonexistent edge must fail loudly rather than land a phantom
			// resource whose next plan is inexplicable.
			ResourceName:      addr,
			ImportState:       true,
			ImportStateIdFunc: importID(sourceURN + "|hasA|" + relatedURN),
			ExpectError:       regexp.MustCompile(`Glossary term relationship not found`),
		},
		{Config: cfg}, // Re-apply so cleanup destroy succeeds.
	}
}

// GlossaryTermRelationshipNonexistentTargetSteps applies an edge whose related
// term does not exist. Related terms are validated referentially at write time
// (this resource's signature server behaviour), so the apply must fail with
// the server's rejection surfaced -- a provider that swallowed it would report
// a created resource backed by no edge.
func GlossaryTermRelationshipNonexistentTargetSteps(sourceID string) []resource.TestStep {
	cfg := providerBlock + fmt.Sprintf(`
resource "datahub_glossary_term" "source" {
  term_id = %q
  name    = "Missing Target Source"
}

resource "datahub_glossary_term_relationship" "test" {
  term_urn          = datahub_glossary_term.source.urn
  relationship_type = "isA"
  related_term_urn  = "urn:li:glossaryTerm:%s-no-such-target"
}
`, sourceID, sourceID)

	return []resource.TestStep{
		{
			Config: cfg,
			// \s+ between words: Terraform hard-wraps diagnostic text at
			// terminal width, so any inter-word space may be a newline.
			ExpectError: regexp.MustCompile(`does\s+not\s+exist`),
		},
	}
}

// GlossaryTermRelationshipInvalidURNSteps verifies the plan-time URN validator.
// The bare-prefix case is the load-bearing one: "urn:li:glossaryTerm:" passes
// every HasPrefix check including the client's, so the schema validator is the
// only guard -- and an empty string interpolation in a URN expression produces
// exactly that value. Both steps fail at validate, before any API call.
func GlossaryTermRelationshipInvalidURNSteps() []resource.TestStep {
	return []resource.TestStep{
		{
			Config: providerBlock + `
resource "datahub_glossary_term_relationship" "test" {
  term_urn          = "urn:li:tag:pii"
  relationship_type = "isA"
  related_term_urn  = "urn:li:glossaryTerm:revenue"
}
`,
			ExpectError: regexp.MustCompile(`Invalid glossary term URN`),
		},
		{
			Config: providerBlock + `
resource "datahub_glossary_term_relationship" "test" {
  term_urn          = "urn:li:glossaryTerm:pii"
  relationship_type = "isA"
  related_term_urn  = "urn:li:glossaryTerm:"
}
`,
			ExpectError: regexp.MustCompile(`Invalid glossary term URN`),
		},
	}
}

// GlossaryTermRelationshipCheckDestroy verifies every
// datahub_glossary_term_relationship in the post-destroy state no longer
// exists as an edge in DataHub.
func GlossaryTermRelationshipCheckDestroy(s *terraform.State) error {
	client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		return fmt.Errorf("CheckDestroy: failed to build DataHub client: %w", err)
	}
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "datahub_glossary_term_relationship" {
			continue
		}
		termURN := rs.Primary.Attributes["term_urn"]
		relationshipType := rs.Primary.Attributes["relationship_type"]
		relatedTermURN := rs.Primary.Attributes["related_term_urn"]
		exists, getErr := client.RelatedTermExists(ctx, termURN, relationshipType, relatedTermURN)
		if getErr != nil {
			return fmt.Errorf("CheckDestroy: unexpected error checking relationship %q -%s-> %q: %w", termURN, relationshipType, relatedTermURN, getErr)
		}
		if exists {
			return fmt.Errorf("relationship %q -%s-> %q still exists after destroy", termURN, relationshipType, relatedTermURN)
		}
	}
	return nil
}
